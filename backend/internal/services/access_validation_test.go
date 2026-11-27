package services_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

// An access captured before a code reset carries the old credential version.
// The game starts with every player at version 0.
func resetPlayer(t *testing.T, s *services.GameService, a models.Access, id string) {
	t.Helper()
	if _, err := s.ResetCode(context.Background(), models.Access{GameID: a.GameID}, id); err != nil {
		t.Fatal(err)
	}
}

func newVersion(a models.Access) models.Access {
	a.Version++
	return a
}

// pendingWithdrawal gives player a a confirmable withdrawal token.
func pendingWithdrawal(t *testing.T, s *services.GameService, a models.Access) string {
	t.Helper()
	request, err := s.RequestWithdrawal(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return request.Token
}

func TestResetBeforeWriteRejectsOldCredentialWithoutChangingState(t *testing.T) {
	ctx := context.Background()
	for _, action := range []string{"report", "respond", "request-withdrawal", "confirm-withdrawal"} {
		t.Run(action, func(t *testing.T) {
			s, repo, a := eliminationGame(t, 4)
			var claimID, token string
			switch action {
			case "respond":
				claim, err := s.ReportElimination(ctx, a[0], "1")
				if err != nil {
					t.Fatal(err)
				}
				claimID = claim.ID
			case "confirm-withdrawal":
				token = pendingWithdrawal(t, s, a[1])
			}
			before, _ := repo.Get(ctx, a[0].GameID)
			target := map[string]string{"report": "0", "respond": "1", "request-withdrawal": "1", "confirm-withdrawal": "1"}[action]
			victim := map[string]int{"report": 0, "respond": 1, "request-withdrawal": 1, "confirm-withdrawal": 1}[action]
			resetPlayer(t, s, a[victim], target)
			mid, _ := repo.Get(ctx, a[0].GameID)

			var err error
			switch action {
			case "report":
				_, err = s.ReportElimination(ctx, a[0], "1")
			case "respond":
				err = s.RespondElimination(ctx, a[1], claimID, true)
			case "request-withdrawal":
				_, err = s.RequestWithdrawal(ctx, a[1])
			case "confirm-withdrawal":
				err = s.ConfirmWithdrawal(ctx, a[1], token)
			}
			if !errors.Is(err, services.ErrUnauthorized) {
				t.Fatalf("old credential: %v, want ErrUnauthorized", err)
			}
			after, _ := repo.Get(ctx, a[0].GameID)
			if after.Revision != mid.Revision || len(after.Eliminations) != len(before.Eliminations) || len(after.Withdrawals) != len(before.Withdrawals) {
				t.Fatal("rejected request changed game state")
			}
			for i, p := range after.Players {
				if p.EliminatedAt != nil || p.WithdrawnAt != nil || p.ConfirmedKills != 0 || p.VictimID != before.Players[i].VictimID {
					t.Fatalf("rejected request changed player %s", p.ID)
				}
			}
		})
	}
}

func TestNewCredentialStillWorksAfterReset(t *testing.T) {
	ctx := context.Background()
	s, _, a := eliminationGame(t, 4)
	resetPlayer(t, s, a[0], "0")
	if _, err := s.ReportElimination(ctx, newVersion(a[0]), "1"); err != nil {
		t.Fatalf("new credential rejected: %v", err)
	}
}

func TestWriteBeforeResetStaysValid(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	claim, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	resetPlayer(t, s, a[0], "0")
	g, _ := repo.Get(ctx, a[0].GameID)
	if len(g.Eliminations) != 1 || g.Eliminations[0].ID != claim.ID || g.Eliminations[0].Status != "pending" {
		t.Fatal("a report committed before the reset must remain")
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatalf("victim response after killer reset: %v", err)
	}
}

// resetOnSave commits a code reset of one player just before the first Save,
// then reports a revision conflict, modelling a reset that wins the race.
type resetOnSave struct {
	*testutil.Memory
	once     sync.Once
	playerID string
}

func (r *resetOnSave) Save(ctx context.Context, g *models.Game, previous int64) error {
	var injected bool
	r.once.Do(func() {
		injected = true
		current, _ := r.Memory.Get(ctx, g.ID)
		for i := range current.Players {
			if current.Players[i].ID == r.playerID {
				current.Players[i].CredentialVersion++
			}
		}
		current.Revision++
		if err := r.Memory.Save(ctx, current, current.Revision-1); err != nil {
			panic(err)
		}
	})
	if injected {
		return services.ErrConflict
	}
	return r.Memory.Save(ctx, g, previous)
}

func TestRetryAfterResetRechecksCredential(t *testing.T) {
	ctx := context.Background()
	_, base, a := eliminationGame(t, 4)
	racing := &resetOnSave{Memory: base, playerID: "0"}
	// A second service instance sharing the repository, as another API process.
	other := services.NewGameService(racing)
	if _, err := other.ReportElimination(ctx, a[0], "1"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("retry after reset: %v, want ErrUnauthorized", err)
	}
	g, _ := base.Get(ctx, a[0].GameID)
	if len(g.Eliminations) != 0 {
		t.Fatal("a report was committed with the revoked credential")
	}
	if g.Players[0].CredentialVersion != 1 {
		t.Fatal("reset was not applied")
	}
}

func TestAccessExpiryIsCheckedAtTheMutation(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	a[0].ExpiresAt = time.Now().Add(time.Hour)
	claim, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatalf("unexpired access rejected: %v", err)
	}
	// Authorized at the HTTP boundary, then the access expired while queued.
	expired := func(a models.Access) models.Access { a.ExpiresAt = time.Now().Add(-time.Second); return a }
	before, _ := repo.Get(ctx, a[0].GameID)
	if _, err = s.RequestWithdrawal(ctx, expired(a[0])); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("expired withdrawal request: %v, want ErrUnauthorized", err)
	}
	if err = s.RespondElimination(ctx, expired(a[1]), claim.ID, true); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("expired response: %v, want ErrUnauthorized", err)
	}
	if _, err = s.ReportElimination(ctx, expired(a[2]), "3"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("expired report: %v, want ErrUnauthorized", err)
	}
	if err = s.ConfirmWithdrawal(ctx, expired(a[1]), "token"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("expired confirmation: %v, want ErrUnauthorized", err)
	}
	after, _ := repo.Get(ctx, a[0].GameID)
	if after.Revision != before.Revision {
		t.Fatal("expired access changed state")
	}
}
