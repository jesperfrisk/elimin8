package controllers

import "net/http"

func (c *GameController) requestWithdrawal(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	request, err := c.service.RequestWithdrawal(r.Context(), a)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusCreated, request)
}

func (c *GameController) confirmWithdrawal(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Token string `json:"withdrawalToken"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if err := c.service.ConfirmWithdrawal(r.Context(), a, in.Token); err != nil {
		gameError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *GameController) registerWithdrawals(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games/{id}/withdrawals", c.requestWithdrawal)
	mux.HandleFunc("POST /api/games/{id}/withdrawals/confirm", c.confirmWithdrawal)
}
