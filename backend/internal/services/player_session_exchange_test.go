package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func rememberedAdmission(t *testing.T, s *services.GameService, repo *testutil.Memory, a models.Access, letter string) (string, string, models.Access) {
	t.Helper()
	ctx := context.Background()
	bearer := strings.Repeat(letter, 48)
	a.Hash, a.ExpiresAt = services.HashToken(bearer), time.Now().Add(time.Hour)
	if err := repo.PutAccess(ctx, a); err != nil {
		t.Fatal(err)
	}
	secret, _, err := s.IssuePlayerSession(ctx, bearer)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := s.ResumePlayerSession(ctx, a.GameID, secret)
	if err != nil {
		t.Fatal(err)
	}
	bound, _, err := s.Authorize(ctx, admission.Token)
	if err != nil || bound.SessionHash != services.HashToken(secret) {
		t.Fatal("resumed admission is not bound to its remembered credential")
	}
	return bearer, secret, bound
}

func TestSessionExchangeFixedDeadlineNoJoinAndGameIsolation(t *testing.T) {
	s, repo, a := eliminationGame(t, 4)
	ctx := context.Background()
	_, secret, _ := rememberedAdmission(t, s, repo, a[0], "A")
	hash := services.HashToken(secret)
	now := time.Now().UTC().Truncate(time.Millisecond)
	record := repo.Sessions[hash]
	record.CreatedAt, record.ExpiresAt = now.Add(-services.RememberDuration+time.Hour), now.Add(time.Hour)
	repo.Sessions[hash] = record
	clock := now
	s.Now = func() time.Time { return clock }
	before, _ := repo.Get(ctx, a[0].GameID)
	for i := 0; i < 2; i++ {
		admission, err := s.ResumePlayerSession(ctx, a[0].GameID, secret)
		if err != nil {
			t.Fatal(err)
		}
		if admission.Role != "player" || admission.Game.Me.ID != a[0].PlayerID || admission.ManagementCode != "" || !admission.ExpiresAt.Equal(record.ExpiresAt) {
			t.Fatal("incorrect identity, role or deadline")
		}
		if admission.Game.Me.Mission != before.Players[0].Mission || admission.Game.Missions != nil || len(admission.Game.Players) != 0 {
			t.Fatal("incorrect role-scoped view")
		}
	}
	after, _ := repo.Get(ctx, a[0].GameID)
	if after.Revision != before.Revision || repo.Sessions[hash].ExpiresAt != record.ExpiresAt {
		t.Fatal("exchange mutated roster or extended remember expiry")
	}
	for _, wrong := range []string{"", strings.Repeat("X", 49), strings.Repeat("X", 48)} {
		if _, err := s.ResumePlayerSession(ctx, a[0].GameID, wrong); !errors.Is(err, services.ErrUnauthorized) {
			t.Fatal("invalid cookie accepted")
		}
	}
	if _, err := s.ResumePlayerSession(ctx, "another-game", secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("cookie crossed games")
	}
	if err := s.ForgetPlayerSession(ctx, "another-game", secret); err != nil || repo.Sessions[hash].RevokedAt != nil {
		t.Fatal("wrong-game delete revoked source cookie")
	}
	admission, err := s.ResumePlayerSession(ctx, a[0].GameID, secret)
	if err != nil {
		t.Fatal(err)
	}
	clock = record.ExpiresAt
	if _, err := s.ResumePlayerSession(ctx, a[0].GameID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("expired remember cookie exchanged")
	}
	if _, _, err := s.Authorize(ctx, admission.Token); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("linked bearer survived remember expiry")
	}
	if _, ok := repo.Sessions[hash]; !ok {
		t.Fatal("expiry must be enforced independently of TTL")
	}
}

func TestSessionRevocationRejectsCachedMutationAccessAndPreservesOtherDevices(t *testing.T) {
	ctx := context.Background()
	for _, action := range []string{"report", "respond", "request-withdrawal", "confirm-withdrawal"} {
		t.Run(action, func(t *testing.T) {
			s, repo, a := eliminationGame(t, 4)
			actor := 0
			var claimID, withdrawal string
			if action == "respond" {
				actor = 1
				claim, err := s.ReportElimination(ctx, a[0], "1")
				if err != nil {
					t.Fatal(err)
				}
				claimID = claim.ID
			}
			if action == "request-withdrawal" || action == "confirm-withdrawal" {
				actor = 1
			}
			if action == "confirm-withdrawal" {
				withdrawal = pendingWithdrawal(t, s, a[actor])
			}
			codeBearer, secret, cached := rememberedAdmission(t, s, repo, a[actor], "A")
			_, otherSecret, other := rememberedAdmission(t, s, repo, a[actor], "B")
			if err := s.ForgetPlayerSession(ctx, a[actor].GameID, secret); err != nil {
				t.Fatal(err)
			}
			before, _ := repo.Get(ctx, a[actor].GameID)
			var err error
			switch action {
			case "report":
				_, err = s.ReportElimination(ctx, cached, "1")
			case "respond":
				err = s.RespondElimination(ctx, cached, claimID, true)
			case "request-withdrawal":
				_, err = s.RequestWithdrawal(ctx, cached)
			case "confirm-withdrawal":
				err = s.ConfirmWithdrawal(ctx, cached, withdrawal)
			}
			if !errors.Is(err, services.ErrUnauthorized) {
				t.Fatalf("cached %s access survived revocation: %v", action, err)
			}
			after, _ := repo.Get(ctx, a[actor].GameID)
			if after.Revision != before.Revision {
				t.Fatal("revoked mutation changed game")
			}
			if _, err := s.ResumePlayerSession(ctx, a[actor].GameID, otherSecret); err != nil {
				t.Fatal("another device lost its credential")
			}
			if _, _, err := s.Authorize(ctx, codeBearer); !errors.Is(err, services.ErrUnauthorized) {
				t.Fatal("initial code-login bearer survived its cookie revocation")
			}
			if _, err := s.RequestWithdrawal(ctx, other); err != nil {
				t.Fatal("another device lost existing access")
			}
		})
	}
}

type exchangeRaceRepository struct {
	*testutil.Memory
	afterAccess func()
	saveError   bool
	onSave      func()
}

func (r *exchangeRaceRepository) PutAccess(ctx context.Context, a models.Access) error {
	if err := r.Memory.PutAccess(ctx, a); err != nil {
		return err
	}
	if r.afterAccess != nil {
		r.afterAccess()
	}
	return nil
}

func (r *exchangeRaceRepository) Save(ctx context.Context, g *models.Game, previous int64) error {
	if r.saveError {
		return errors.New("temporary revision fence failure")
	}
	if r.onSave != nil {
		callback := r.onSave
		r.onSave = nil
		callback()
		return services.ErrConflict
	}
	return r.Memory.Save(ctx, g, previous)
}

func TestSessionExchangeRechecksConcurrentResetAndRevocation(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"reset", "revoke"} {
		t.Run(change, func(t *testing.T) {
			s, base, a := eliminationGame(t, 4)
			_, secret, _ := rememberedAdmission(t, s, base, a[0], "A")
			race := &exchangeRaceRepository{Memory: base}
			race.afterAccess = func() {
				if change == "reset" {
					resetPlayer(t, s, a[0], "0")
				} else if err := s.ForgetPlayerSession(ctx, a[0].GameID, secret); err != nil {
					t.Fatal(err)
				}
			}
			other := services.NewGameService(race)
			admission, err := other.ResumePlayerSession(ctx, a[0].GameID, secret)
			if !errors.Is(err, services.ErrUnauthorized) || admission.Token != "" {
				t.Fatal("concurrently invalidated exchange returned admission")
			}
			if _, err := other.ResumePlayerSession(ctx, a[0].GameID, secret); !errors.Is(err, services.ErrUnauthorized) {
				t.Fatal("invalidated cookie resumed on retry")
			}
		})
	}
}

func TestSessionRevocationFenceSurvivesFailureAndMutationRetries(t *testing.T) {
	ctx := context.Background()
	s, base, a := eliminationGame(t, 4)
	_, secret, cached := rememberedAdmission(t, s, base, a[0], "A")
	race := &exchangeRaceRepository{Memory: base, saveError: true}
	other := services.NewGameService(race)
	if err := other.ForgetPlayerSession(ctx, a[0].GameID, secret); err == nil {
		t.Fatal("failed fence acknowledged success")
	}
	marker := base.Sessions[services.HashToken(secret)]
	if marker.RevokedAt == nil {
		t.Fatal("failure lost durable revocation marker")
	}
	if _, err := s.RequestWithdrawal(ctx, cached); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("failed fence reactivated access")
	}
	race.saveError = false
	if err := other.ForgetPlayerSession(ctx, a[0].GameID, secret); err != nil {
		t.Fatal(err)
	}
	if err := other.ForgetPlayerSession(ctx, a[0].GameID, secret); err != nil {
		t.Fatal(err)
	}
	if got := base.Sessions[marker.Hash]; got.ExpiresAt != marker.ExpiresAt || !got.RevokedAt.Equal(*marker.RevokedAt) {
		t.Fatal("retry extended expiry or rewrote revocation time")
	}
	_, secondSecret, second := rememberedAdmission(t, s, base, a[0], "B")
	race.onSave = func() {
		if err := s.ForgetPlayerSession(ctx, a[0].GameID, secondSecret); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := other.ReportElimination(ctx, second, "1"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("mutation retry ignored completed revocation")
	}
	g, _ := base.Get(ctx, a[0].GameID)
	if len(g.Eliminations) != 0 {
		t.Fatal("revoked mutation committed")
	}
}

func TestSessionResumeSupportsEndedPlayersAndRejectsExcludedInvitees(t *testing.T) {
	s, repo, a := eliminationGame(t, 4)
	ctx := context.Background()
	_, secret, _ := rememberedAdmission(t, s, repo, a[0], "A")
	for _, status := range []string{"eliminated", "withdrawn", "ended", "excluded"} {
		g, _ := repo.Get(ctx, a[0].GameID)
		previous := g.Revision
		now := time.Now().UTC()
		switch status {
		case "eliminated":
			g.Players[0].EliminatedAt = &now
		case "withdrawn":
			g.Players[0].WithdrawnAt = &now
		case "ended":
			g.Status, g.EndedAt = "ended", &now
		case "excluded":
			g.Players[0].Included = false
		}
		g.Revision++
		if err := repo.Save(ctx, g, previous); err != nil {
			t.Fatal(err)
		}
		admission, err := s.ResumePlayerSession(ctx, a[0].GameID, secret)
		if status == "excluded" {
			if !errors.Is(err, services.ErrUnauthorized) {
				t.Fatal("excluded invitee resumed")
			}
			continue
		}
		if err != nil || admission.Game.Me.ID != "0" {
			t.Fatal("admitted player lost access")
		}
	}
}

func TestSessionRevocationIsRecheckedAcrossPhotoStorageIO(t *testing.T) {
	ctx := context.Background()
	for _, duringIO := range []bool{false, true} {
		f := newPhotoFixture(t)
		_, secret, cached := rememberedAdmission(t, f.games, f.repo, f.player, "A")
		if duringIO {
			f.store.afterPut = func() {
				if err := f.games.ForgetPlayerSession(ctx, "g1", secret); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			if err := f.games.ForgetPlayerSession(ctx, "g1", secret); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.photos.Put(ctx, cached, "p0", photoPNG(t)); !errors.Is(err, services.ErrUnauthorized) {
			t.Fatal("revoked admission uploaded a photo")
		}
		if err := f.photos.Delete(ctx, cached, "p0"); !errors.Is(err, services.ErrUnauthorized) {
			t.Fatal("revoked admission deleted a photo")
		}
		if len(f.store.objects) != 0 || f.game(t).Players[0].PhotoID != "" {
			t.Fatal("revoked upload left a photo reference or staged object")
		}
	}
}
