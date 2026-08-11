package game

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

const (
	TriviaBaseScore           = 100
	triviaChoiceCount         = 4
	maxTriviaAliases          = 12
	maxTriviaAnswerRunes      = 120
	maxTriviaOptionRunes      = 120
	maxTriviaExplanationRunes = 600
	maxTriviaSourceRunes      = 300
	maxTriviaOptionIDRunes    = 80
)

type triviaHashPayload struct {
	Version         int                   `json:"version"`
	Kind            models.GameKind       `json:"kind"`
	CanonicalAnswer string                `json:"canonicalAnswer"`
	AcceptedAliases []string              `json:"acceptedAliases"`
	BaseScore       int                   `json:"baseScore"`
	Explanation     string                `json:"explanation"`
	Source          string                `json:"source"`
	Options         []models.TriviaOption `json:"options"`
	CorrectOptionID string                `json:"correctOptionId"`
}

func ComputeTriviaContentHash(content models.TriviaContent) string {
	payload, _ := json.Marshal(triviaHashPayload{
		Version: content.Version, Kind: content.Kind, CanonicalAnswer: content.CanonicalAnswer,
		AcceptedAliases: content.AcceptedAliases, BaseScore: content.BaseScore,
		Explanation: content.Explanation, Source: content.Source, Options: content.Options,
		CorrectOptionID: content.CorrectOptionID,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func ValidateTriviaContent(content models.TriviaContent) error {
	if content.Version != models.TriviaContentVersion {
		return triviaInvalid("version must be %d", models.TriviaContentVersion)
	}
	if content.Kind != models.GameKindTriviaOpen && content.Kind != models.GameKindTriviaChoice {
		return triviaInvalid("kind must be trivia_open or trivia_choice")
	}
	if err := boundedRequired("canonical answer", content.CanonicalAnswer, maxTriviaAnswerRunes); err != nil {
		return err
	}
	if content.BaseScore != TriviaBaseScore {
		return triviaInvalid("base score must be %d", TriviaBaseScore)
	}
	if utf8.RuneCountInString(content.Explanation) > maxTriviaExplanationRunes {
		return triviaInvalid("explanation exceeds %d characters", maxTriviaExplanationRunes)
	}
	if utf8.RuneCountInString(content.Source) > maxTriviaSourceRunes {
		return triviaInvalid("source exceeds %d characters", maxTriviaSourceRunes)
	}
	if len(content.AcceptedAliases) > maxTriviaAliases {
		return triviaInvalid("accepted aliases exceed %d", maxTriviaAliases)
	}
	answers := map[string]struct{}{normalizeAnswer(content.CanonicalAnswer): {}}
	for _, alias := range content.AcceptedAliases {
		if err := boundedRequired("accepted alias", alias, maxTriviaAnswerRunes); err != nil {
			return err
		}
		normalized := normalizeAnswer(alias)
		if _, exists := answers[normalized]; exists {
			return triviaInvalid("canonical answer and aliases must be normalization-distinct")
		}
		answers[normalized] = struct{}{}
	}

	if content.Kind == models.GameKindTriviaOpen {
		if len(content.Options) != 0 || content.CorrectOptionID != "" {
			return triviaInvalid("open trivia cannot contain choice options or a correct option ID")
		}
	} else {
		if len(content.Options) != triviaChoiceCount {
			return triviaInvalid("choice trivia must contain exactly %d options", triviaChoiceCount)
		}
		labels, ids, correct := map[string]struct{}{}, map[string]struct{}{}, false
		for _, option := range content.Options {
			if err := boundedRequired("option ID", option.ID, maxTriviaOptionIDRunes); err != nil {
				return err
			}
			if err := boundedRequired("option label", option.Label, maxTriviaOptionRunes); err != nil {
				return err
			}
			if _, exists := ids[option.ID]; exists {
				return triviaInvalid("choice option IDs must be unique")
			}
			ids[option.ID] = struct{}{}
			normalized := normalizeAnswer(option.Label)
			if _, exists := labels[normalized]; exists {
				return triviaInvalid("choice labels must be normalization-distinct")
			}
			labels[normalized] = struct{}{}
			correct = correct || option.ID == content.CorrectOptionID
		}
		if !correct {
			return triviaInvalid("correct option ID must belong to the option set")
		}
	}
	if content.IntegrityHash == "" || content.IntegrityHash != ComputeTriviaContentHash(content) {
		return triviaInvalid("integrity hash does not match frozen content")
	}
	return nil
}

func ProjectTriviaContent(content *models.TriviaContent, revealed bool) *models.PublicTriviaContent {
	if content == nil {
		return nil
	}
	projected := &models.PublicTriviaContent{Version: content.Version, Kind: content.Kind, BaseScore: content.BaseScore}
	if content.Kind == models.GameKindTriviaChoice {
		projected.Options = append([]models.TriviaOption(nil), content.Options...)
	}
	if revealed {
		projected.CanonicalAnswer = content.CanonicalAnswer
		projected.AcceptedAliases = append([]string(nil), content.AcceptedAliases...)
		projected.CorrectOptionID = content.CorrectOptionID
		projected.Explanation = content.Explanation
		projected.Source = content.Source
	}
	return projected
}

func boundedRequired(label, value string, maximum int) error {
	length := utf8.RuneCountInString(strings.TrimSpace(value))
	if length < 1 || length > maximum {
		return triviaInvalid("%s must contain 1-%d characters", label, maximum)
	}
	return nil
}

func triviaInvalid(format string, arguments ...any) error {
	return fmt.Errorf("%w: trivia content: %s", ErrGeneratedContentInvalid, fmt.Sprintf(format, arguments...))
}
