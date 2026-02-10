package server

import "testing"

func TestValidateHTTPSRequirement(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "valid https",
			baseURL: "https://klaus.example.com",
			wantErr: false,
		},
		{
			name:    "http localhost allowed",
			baseURL: "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "http 127.0.0.1 allowed",
			baseURL: "http://127.0.0.1:8080",
			wantErr: false,
		},
		{
			name:    "http ::1 allowed",
			baseURL: "http://[::1]:8080",
			wantErr: false,
		},
		{
			name:    "http non-loopback rejected",
			baseURL: "http://example.com",
			wantErr: true,
		},
		{
			name:    "empty URL",
			baseURL: "",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			baseURL: "ftp://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTTPSRequirement(tt.baseURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHTTPSRequirement(%q) error = %v, wantErr %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}
}
