package services

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strconv"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// ErrTeamSetup means the teams are not ready for a team-mode start.
var ErrTeamSetup = errors.New("team games need every joined player on a team and at least two teams with players")

const maxTeams = 50

// TeamInput is one team in a full-replace request. An empty ID gets a new one.
type TeamInput struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	PlayerIDs []string `json:"playerIds"`
}

func lobbyOrganizer(g *models.Game, a models.Access) error {
	if a.PlayerID != "" {
		return ErrForbidden
	}
	switch g.Status {
	case "lobby":
		return nil
	case "ended":
		return ErrGameEnded
	}
	return ErrAlreadyStarted
}

func (s *GameService) SetMode(ctx context.Context, a models.Access, mode models.GameMode) error {
	if mode != models.ModeIndividual && mode != models.ModeTeam {
		return ErrInvalid
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if err := lobbyOrganizer(g, a); err != nil {
			return err
		}
		g.Mode = mode
		return nil
	})
	return err
}

// SetTeams replaces all teams and memberships. Players omitted from every team
// are unassigned. Only valid in team mode while in the lobby.
func (s *GameService) SetTeams(ctx context.Context, a models.Access, in []TeamInput) ([]models.TeamSetupView, error) {
	if len(in) > maxTeams {
		return nil, ErrInvalid
	}
	var out []models.TeamSetupView
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if err := lobbyOrganizer(g, a); err != nil {
			return err
		}
		if g.Mode != models.ModeTeam {
			return ErrInvalid
		}
		teams := make([]models.Team, 0, len(in))
		assigned := map[string]string{}
		known := map[string]bool{}
		for _, p := range g.Players {
			known[p.ID] = true
		}
		ids := map[string]bool{}
		for _, t := range in {
			if !validText(t.Name, 40) {
				return ErrInvalid
			}
			id := t.ID
			if id == "" {
				var err error
				if id, err = RandomCode(12); err != nil {
					return err
				}
			}
			if ids[id] {
				return ErrInvalid
			}
			ids[id] = true
			for _, pid := range t.PlayerIDs {
				if !known[pid] || assigned[pid] != "" {
					return ErrInvalid
				}
				assigned[pid] = id
			}
			teams = append(teams, models.Team{ID: id, Name: t.Name})
		}
		applyTeams(g, teams, assigned)
		out = setupViews(g)
		return nil
	})
	return out, err
}

func applyTeams(g *models.Game, teams []models.Team, assigned map[string]string) {
	g.Teams = teams
	for i := range g.Players {
		g.Players[i].TeamID = assigned[g.Players[i].ID]
	}
}

func setupViews(g *models.Game) []models.TeamSetupView {
	out := make([]models.TeamSetupView, 0, len(g.Teams))
	for _, t := range g.Teams {
		v := models.TeamSetupView{ID: t.ID, Name: t.Name, PlayerIDs: []string{}}
		for _, p := range g.Players {
			if p.TeamID == t.ID {
				v.PlayerIDs = append(v.PlayerIDs, p.ID)
			}
		}
		out = append(out, v)
	}
	return out
}

// RandomTeams deals shuffled ids round-robin into count teams, so sizes differ
// by at most one. intn(n) must return a value in [0, n); it is injectable so
// tests are deterministic.
func RandomTeams(ids []string, count int, intn func(int) int) [][]string {
	shuffled := append([]string{}, ids...)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	teams := make([][]string, count)
	for i, id := range shuffled {
		teams[i%count] = append(teams[i%count], id)
	}
	return teams
}

func cryptoIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return int(v.Int64())
}

// RandomizeTeams replaces the teams with count balanced teams of the joined
// players, named "Team 1".."Team N", and persists them.
func (s *GameService) RandomizeTeams(ctx context.Context, a models.Access, count int) ([]models.TeamSetupView, error) {
	var out []models.TeamSetupView
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if err := lobbyOrganizer(g, a); err != nil {
			return err
		}
		if g.Mode != models.ModeTeam {
			return ErrInvalid
		}
		var joined []string
		for _, p := range g.Players {
			if p.JoinedAt != nil {
				joined = append(joined, p.ID)
			}
		}
		if count < 2 || count > len(joined) {
			return ErrInvalid
		}
		intn := s.Intn
		if intn == nil {
			intn = cryptoIntn
		}
		teams := make([]models.Team, count)
		assigned := map[string]string{}
		for i, members := range RandomTeams(joined, count, intn) {
			id, err := RandomCode(12)
			if err != nil {
				return err
			}
			teams[i] = models.Team{ID: id, Name: "Team " + strconv.Itoa(i+1)}
			for _, pid := range members {
				assigned[pid] = id
			}
		}
		applyTeams(g, teams, assigned)
		out = setupViews(g)
		return nil
	})
	return out, err
}

// validateTeamStart is called from the start path in team mode.
func validateTeamStart(g *models.Game) error {
	valid := map[string]bool{}
	for _, t := range g.Teams {
		valid[t.ID] = true
	}
	populated := map[string]bool{}
	for _, p := range g.Players {
		if p.JoinedAt == nil {
			continue
		}
		if !valid[p.TeamID] {
			return ErrTeamSetup
		}
		populated[p.TeamID] = true
	}
	if len(populated) < 2 {
		return ErrTeamSetup
	}
	return nil
}

// teamViewFor returns the player's own team only; nil when none applies.
func teamViewFor(g *models.Game, playerID string) *models.TeamView {
	if g.Mode != models.ModeTeam {
		return nil
	}
	var mine *models.Player
	for i := range g.Players {
		if g.Players[i].ID == playerID {
			mine = &g.Players[i]
		}
	}
	if mine == nil || mine.TeamID == "" {
		return nil
	}
	view := &models.TeamView{Members: []models.VictimView{}}
	for _, t := range g.Teams {
		if t.ID == mine.TeamID {
			view.Name = t.Name
		}
	}
	for _, p := range g.Players {
		if p.TeamID != mine.TeamID {
			continue
		}
		// Lobby setup may assign invitees who never join. Starting freezes the
		// roster to the joined participants, and an excluded invitee must not
		// appear as a teammate afterwards. Included teammates stay listed after
		// they are eliminated or withdraw, so this checks inclusion, not life.
		if g.StartedAt != nil && !p.Included {
			continue
		}
		view.Members = append(view.Members, models.VictimView{ID: p.ID, Name: p.Name})
	}
	return view
}
