package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bogdandobrica/modelsays/backend/internal/db"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type bankFile struct {
	Version      int         `json:"version"`
	BankName     string      `json:"bankName"`
	ReviewStatus string      `json:"reviewStatus"`
	Entries      []bankEntry `json:"entries"`
}

type bankEntry struct {
	ID              string                    `json:"id"`
	GameKind        models.GameKind           `json:"gameKind"`
	Locale          string                    `json:"locale"`
	Category        string                    `json:"category"`
	Question        string                    `json:"question"`
	CanonicalAnswer string                    `json:"canonicalAnswer"`
	AcceptedAliases []string                  `json:"acceptedAliases"`
	Options         []models.TriviaOption     `json:"options"`
	CorrectOptionID string                    `json:"correctOptionId"`
	Answers         []models.PredictionAnswer `json:"answers"`
	BaseScore       int                       `json:"baseScore"`
	Explanation     string                    `json:"explanation"`
	Source          string                    `json:"source"`
	ReviewStatus    string                    `json:"reviewStatus"`
}

func main() {
	var path, databaseURL string
	var allowUnreviewed bool
	flag.StringVar(&path, "file", "", "bank JSON file")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	flag.BoolVar(&allowUnreviewed, "allow-unreviewed", false, "load unreviewed entries for private testing")
	flag.Parse()
	if path == "" || databaseURL == "" {
		fatal("-file and -database-url are required")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		fatal("read bank: %v", err)
	}
	var bank bankFile
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bank); err != nil {
		fatal("decode bank: %v", err)
	}
	if bank.Version < 1 || strings.TrimSpace(bank.BankName) == "" || len(bank.Entries) == 0 {
		fatal("bank requires a positive version, bankName, and entries")
	}
	ids := make(map[string]struct{}, len(bank.Entries))
	approved := make([]bankEntry, 0, len(bank.Entries))
	skipped := 0
	for index := range bank.Entries {
		if bank.Entries[index].ReviewStatus == "" {
			bank.Entries[index].ReviewStatus = bank.ReviewStatus
		}
		if bank.ReviewStatus == "reviewed" && bank.Entries[index].ReviewStatus == "unreviewed" && !allowUnreviewed {
			skipped++
			continue
		}
		if err := validateEntry(&bank.Entries[index], allowUnreviewed); err != nil {
			fatal("entry %d (%s): %v", index+1, bank.Entries[index].ID, err)
		}
		if _, exists := ids[bank.Entries[index].ID]; exists {
			fatal("duplicate entry ID %q", bank.Entries[index].ID)
		}
		ids[bank.Entries[index].ID] = struct{}{}
		approved = append(approved, bank.Entries[index])
	}
	if len(approved) == 0 {
		fatal("bank contains no approved entries")
	}
	bank.Entries = approved

	ctx := context.Background()
	pool, err := db.OpenPostgresPool(ctx, databaseURL)
	if err != nil {
		fatal("connect database: %v", err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		fatal("begin import: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE content_bank_items SET enabled=false WHERE bank_name=$1`, bank.BankName); err != nil {
		fatal("disable previous bank revision: %v", err)
	}
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	for _, entry := range bank.Entries {
		payload, _ := json.Marshal(entry)
		_, err := tx.Exec(ctx, `
			INSERT INTO content_bank_items
			(id,bank_name,bank_version,game_kind,locale,category,question_text,payload_jsonb,review_status,enabled,source_sha256,imported_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10,$11)
			ON CONFLICT (id) DO UPDATE SET bank_name=EXCLUDED.bank_name,bank_version=EXCLUDED.bank_version,
			game_kind=EXCLUDED.game_kind,locale=EXCLUDED.locale,category=EXCLUDED.category,
			question_text=EXCLUDED.question_text,payload_jsonb=EXCLUDED.payload_jsonb,
			review_status=EXCLUDED.review_status,enabled=true,source_sha256=EXCLUDED.source_sha256,imported_at=EXCLUDED.imported_at`,
			entry.ID, bank.BankName, bank.Version, entry.GameKind, entry.Locale, entry.Category,
			entry.Question, payload, entry.ReviewStatus, fingerprint, time.Now().UTC())
		if err != nil {
			fatal("store entry %s: %v", entry.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		fatal("commit import: %v", err)
	}
	fmt.Printf("Loaded and activated %d items from bank %q version %d", len(bank.Entries), bank.BankName, bank.Version)
	if skipped > 0 {
		fmt.Printf("; skipped %d unreviewed items", skipped)
	}
	fmt.Println(".")
}

func validateEntry(entry *bankEntry, allowUnreviewed bool) error {
	entry.ID, entry.Locale, entry.Category, entry.Question = strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Locale), strings.TrimSpace(entry.Category), strings.TrimSpace(entry.Question)
	if entry.ID == "" || entry.Locale == "" || entry.Category == "" || entry.Question == "" {
		return fmt.Errorf("id, locale, category, and question are required")
	}
	if utf8.RuneCountInString(entry.Question) > 500 {
		return fmt.Errorf("question exceeds 500 characters")
	}
	if entry.ReviewStatus != "reviewed" && entry.ReviewStatus != "unreviewed" {
		return fmt.Errorf("reviewStatus must be reviewed or unreviewed")
	}
	if entry.ReviewStatus != "reviewed" && !allowUnreviewed {
		return fmt.Errorf("entry is unreviewed; finish review or set ALLOW_UNREVIEWED=yes")
	}
	switch entry.GameKind {
	case models.GameKindTriviaOpen, models.GameKindTriviaChoice:
		content := models.TriviaContent{Version: models.TriviaContentVersion, Kind: entry.GameKind,
			CanonicalAnswer: entry.CanonicalAnswer, AcceptedAliases: entry.AcceptedAliases,
			BaseScore: entry.BaseScore, Explanation: entry.Explanation, Source: entry.Source,
			Options: entry.Options, CorrectOptionID: entry.CorrectOptionID}
		content.IntegrityHash = game.ComputeTriviaContentHash(content)
		if err := game.ValidateTriviaContent(content); err != nil {
			return err
		}
		if len(entry.Answers) != 0 {
			return fmt.Errorf("trivia entries cannot contain answers")
		}
	case models.GameKindModelSays:
		if entry.CanonicalAnswer != "" || len(entry.Options) != 0 || entry.CorrectOptionID != "" {
			return fmt.Errorf("model_says entries must use answers, not trivia solution fields")
		}
		board := models.PredictionBoard{Provider: "database", ModelName: "content-bank", PromptVersion: "bank-v1", Answers: entry.Answers}
		if err := game.ValidatePredictionBoard(board); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported gameKind %q", entry.GameKind)
	}
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "load bank: "+format+"\n", args...)
	os.Exit(1)
}
