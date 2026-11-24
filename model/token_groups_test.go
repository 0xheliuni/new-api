package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenNormalizeLegacyGroups(t *testing.T) {
	cases := []struct {
		name        string
		token       Token
		changed     bool
		wantGroup   string
		wantAuto    string
		wantRetries bool
	}{
		{name: "empty is untouched", token: Token{Group: ""}, wantGroup: ""},
		{name: "single group is untouched", token: Token{Group: "vip"}, wantGroup: "vip"},
		{name: "stored auto list is untouched", token: Token{Group: "auto", AutoGroups: `["vip"]`}, wantGroup: "auto", wantAuto: `["vip"]`},
		{name: "list becomes an ordered auto list with retry", token: Token{Group: " claude , gpt "}, changed: true, wantGroup: "auto", wantAuto: `["claude","gpt"]`, wantRetries: true},
		{name: "empty entries and duplicates are dropped", token: Token{Group: "claude,,gpt,claude,"}, changed: true, wantGroup: "auto", wantAuto: `["claude","gpt"]`, wantRetries: true},
		{name: "one remaining group becomes a plain group", token: Token{Group: "claude,claude", CrossGroupRetry: true}, changed: true, wantGroup: "claude"},
		{name: "auto pseudo group is dropped from a list", token: Token{Group: "auto,claude"}, changed: true, wantGroup: "claude"},
		{name: "auto alone keeps inheriting", token: Token{Group: "auto,"}, changed: true, wantGroup: "auto"},
		{name: "separators only follow the user group", token: Token{Group: ",,"}, changed: true, wantGroup: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := tc.token
			assert.Equal(t, tc.changed, token.NormalizeLegacyGroups())
			assert.Equal(t, tc.wantGroup, token.Group)
			if tc.wantAuto == "" {
				assert.Empty(t, token.AutoGroups)
			} else {
				assert.JSONEq(t, tc.wantAuto, token.AutoGroups)
			}
			assert.Equal(t, tc.wantRetries, token.CrossGroupRetry)
		})
	}
}
