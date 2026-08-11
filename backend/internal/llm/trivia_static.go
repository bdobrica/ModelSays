package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

const curatedTriviaBaseScore = 100

type curatedTriviaEntry struct {
	question models.Question
	content  models.TriviaContent
}

func openTrivia(id, question, answer string, aliases []string, explanation string) curatedTriviaEntry {
	return curatedTriviaEntry{
		question: models.Question{ID: id, Text: question, Locale: "en", Category: "general_knowledge"},
		content: models.TriviaContent{Version: models.TriviaContentVersion, Kind: models.GameKindTriviaOpen,
			CanonicalAnswer: answer, AcceptedAliases: aliases, BaseScore: curatedTriviaBaseScore, Explanation: explanation, Source: "Model Says reviewed offline bank"},
	}
}

func choiceTrivia(id, question, correct string, labels ...string) curatedTriviaEntry {
	options := make([]models.TriviaOption, len(labels))
	correctID := ""
	for index, label := range labels {
		options[index] = models.TriviaOption{ID: fmt.Sprintf("%s-o%d", id, index+1), Label: label}
		if label == correct {
			correctID = options[index].ID
		}
	}
	return curatedTriviaEntry{
		question: models.Question{ID: id, Text: question, Locale: "en", Category: "general_knowledge"},
		content: models.TriviaContent{Version: models.TriviaContentVersion, Kind: models.GameKindTriviaChoice,
			CanonicalAnswer: correct, BaseScore: curatedTriviaBaseScore, Options: options, CorrectOptionID: correctID,
			Explanation: correct + " is the correct answer.", Source: "Model Says reviewed offline bank"},
	}
}

var curatedTriviaByKindAndLocale = map[models.GameKind]map[string][]curatedTriviaEntry{
	models.GameKindTriviaOpen: {"en": {
		openTrivia("trivia-open-en-001", "What is the largest planet in our solar system?", "Jupiter", nil, "Jupiter is the solar system's largest planet."),
		openTrivia("trivia-open-en-002", "Which element has the chemical symbol Au?", "gold", nil, "Au comes from the Latin name aurum."),
		openTrivia("trivia-open-en-003", "What is the capital city of Japan?", "Tokyo", nil, "Tokyo is Japan's capital and largest city."),
		openTrivia("trivia-open-en-004", "Who painted the Mona Lisa?", "Leonardo da Vinci", []string{"da Vinci", "Leonardo"}, "Leonardo da Vinci painted the Mona Lisa."),
		openTrivia("trivia-open-en-005", "How many sides does a hexagon have?", "six", []string{"6"}, "A hexagon is a six-sided polygon."),
	}},
	models.GameKindTriviaChoice: {"en": {
		choiceTrivia("trivia-choice-en-001", "Which ocean is the largest?", "Pacific Ocean", "Atlantic Ocean", "Indian Ocean", "Pacific Ocean", "Arctic Ocean"),
		choiceTrivia("trivia-choice-en-002", "Which planet is known as the Red Planet?", "Mars", "Venus", "Mars", "Mercury", "Saturn"),
		choiceTrivia("trivia-choice-en-003", "In which continent is the Sahara Desert?", "Africa", "Asia", "Africa", "Australia", "South America"),
		choiceTrivia("trivia-choice-en-004", "What is the freezing point of water in Celsius?", "0 degrees", "0 degrees", "10 degrees", "32 degrees", "100 degrees"),
		choiceTrivia("trivia-choice-en-005", "Which instrument typically has 88 keys?", "Piano", "Violin", "Piano", "Flute", "Trumpet"),
	}},
}

func (client *StaticModelClient) GenerateTrivia(_ context.Context, req GenerateTriviaRequest) (*GenerateTriviaResponse, error) {
	locale := strings.TrimSpace(req.Locale)
	entries := curatedTriviaByKindAndLocale[req.Kind][locale]
	if len(entries) == 0 && locale != "en" {
		return nil, fmt.Errorf("no curated trivia available for locale %q", locale)
	}
	available := make([]curatedTriviaEntry, 0, len(entries))
	for _, entry := range entries {
		if !containsFold(req.ExcludedText, entry.question.Text) {
			available = append(available, entry)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no unused curated trivia available for %s", req.Kind)
	}
	randomIndex := client.randomIndex
	if randomIndex == nil {
		randomIndex = cryptographicRandomIndex
	}
	selected, err := randomIndex(len(available))
	if err != nil {
		return nil, fmt.Errorf("select curated trivia: %w", err)
	}
	entry := available[selected]
	entry.content.AcceptedAliases = append([]string(nil), entry.content.AcceptedAliases...)
	entry.content.Options = append([]models.TriviaOption(nil), entry.content.Options...)
	return &GenerateTriviaResponse{Question: entry.question, Content: entry.content, Metadata: CallMetadata{
		Provider: "static", Model: "curated-trivia-bank", PromptVersion: TriviaPromptVersion,
	}}, nil
}
