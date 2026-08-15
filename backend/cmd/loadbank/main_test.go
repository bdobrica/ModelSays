package main

import (
	"testing"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

func TestValidateEntrySupportsEveryGameKind(t *testing.T) {
	tests := []bankEntry{
		{ID: "ms-1", GameKind: models.GameKindModelSays, Locale: "en", Category: "party", Question: "Name something.", ReviewStatus: "reviewed",
			Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "one", Rank: 1, Score: 50}, {CanonicalAnswer: "two", Rank: 2, Score: 40},
				{CanonicalAnswer: "three", Rank: 3, Score: 30}, {CanonicalAnswer: "four", Rank: 4, Score: 20},
				{CanonicalAnswer: "five", Rank: 5, Score: 10},
			}},
		{ID: "open-1", GameKind: models.GameKindTriviaOpen, Locale: "en", Category: "general", Question: "Capital of France?", ReviewStatus: "reviewed",
			CanonicalAnswer: "Paris", BaseScore: 100},
		{ID: "choice-1", GameKind: models.GameKindTriviaChoice, Locale: "ro", Category: "general", Question: "Capitala Franței?", ReviewStatus: "reviewed",
			CanonicalAnswer: "Paris", BaseScore: 100, CorrectOptionID: "o2", Options: []models.TriviaOption{
				{ID: "o1", Label: "Roma"}, {ID: "o2", Label: "Paris"}, {ID: "o3", Label: "Oslo"}, {ID: "o4", Label: "Lima"},
			}},
	}
	for _, entry := range tests {
		entry := entry
		t.Run(string(entry.GameKind), func(t *testing.T) {
			if err := validateEntry(&entry, false); err != nil {
				t.Fatalf("validateEntry: %v", err)
			}
		})
	}
}

func TestValidateEntryRequiresReviewByDefault(t *testing.T) {
	entry := bankEntry{ID: "open-1", GameKind: models.GameKindTriviaOpen, Locale: "en", Category: "general",
		Question: "Capital of France?", CanonicalAnswer: "Paris", BaseScore: 100, ReviewStatus: "unreviewed"}
	if err := validateEntry(&entry, false); err == nil {
		t.Fatal("unreviewed entry was accepted without override")
	}
	if err := validateEntry(&entry, true); err != nil {
		t.Fatalf("private-test override: %v", err)
	}
}
