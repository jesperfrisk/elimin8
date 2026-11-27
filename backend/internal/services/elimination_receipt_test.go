package services_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestEliminationReceiptVictimOnlyAndDurable(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	claim, err := s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if services.View(g, a[1]).Me.Elimination != nil {
		t.Fatal("receipt shown while pending")
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	// Later inherited missions must not change the historical notice.
	g.Players[0].Mission = "Later mission"
	r := services.View(g, a[1]).Me.Elimination
	if r == nil || r.Killer.ID != "0" || r.Killer.Name != "Player 0" || r.Mission == nil || *r.Mission != "Mission 0" || r.ConfirmedAt.IsZero() {
		t.Fatalf("bad receipt: %+v", r)
	}
	for _, actor := range []models.Access{a[0], a[2], a[3], {GameID: a[0].GameID}} {
		data, _ := json.Marshal(services.View(g, actor))
		if strings.Contains(string(data), `"elimination"`) || strings.Contains(string(data), `"Mission 0"`) {
			t.Fatalf("receipt/mission leaked to %q: %s", actor.PlayerID, data)
		}
	}
}

func TestEliminationReceiptNotForDeclinedOrLegacy(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 3)
	claim, _ := s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err := s.RespondElimination(ctx, a[1], claim.ID, false); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if services.View(g, a[1]).Me.Elimination != nil {
		t.Fatal("declined claim produced receipt")
	}
	claim, _ = s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err := s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	for i := range g.Eliminations {
		g.Eliminations[i].Mission = "" // legacy record
	}
	r := services.View(g, a[1]).Me.Elimination
	if r == nil || r.Mission != nil {
		t.Fatalf("legacy receipt should have null mission: %+v", r)
	}
	data, _ := json.Marshal(r)
	if !strings.Contains(string(data), `"mission":null`) {
		t.Fatalf("mission not null: %s", data)
	}
}

func TestEliminationReceiptOnGameEndingKill(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 2)
	claim, _ := s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err := s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Status != "ended" {
		t.Fatalf("expected ended game, got %s", g.Status)
	}
	if r := services.View(g, a[1]).Me.Elimination; r == nil || r.Mission == nil || *r.Mission != "Mission 0" {
		t.Fatalf("no receipt after final elimination: %+v", r)
	}
}
