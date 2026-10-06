package cmd

import (
	"testing"

	"github.com/giantswarm/klaus/pkg/config"
)

func TestCheckAuthConfig(t *testing.T) {
	const issuer = "https://dex.example.com"
	tests := []struct {
		name    string
		server  config.ServerConfig
		oauth   bool
		wantErr bool
	}{
		{name: "OAuth with an owner", server: config.ServerConfig{OwnerSubject: "owner"}, oauth: true},
		{name: "issuer and audience", server: config.ServerConfig{TokenIssuerURL: issuer, TokenAudiences: []string{"muster"}}},
		{name: "issuer, audience and owner", server: config.ServerConfig{OwnerSubject: "owner", TokenIssuerURL: issuer, TokenAudiences: []string{"muster"}}},
		{name: "issuer without audience", server: config.ServerConfig{TokenIssuerURL: issuer}, wantErr: true},
		{name: "owner without a token issuer", server: config.ServerConfig{OwnerSubject: "owner"}, wantErr: true},
		{name: "owner without a token issuer, opted in to no authentication", server: config.ServerConfig{OwnerSubject: "owner", AllowUnauthenticated: true}, wantErr: true},
		{name: "no authentication", server: config.ServerConfig{}, wantErr: true},
		{name: "no authentication, opted in", server: config.ServerConfig{AllowUnauthenticated: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAuthConfig(tc.server, tc.oauth)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkAuthConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
