package utils

import "testing"

func TestGetExternalIP(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "valid response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetExternalIP()
			if err != nil {
				t.Errorf("GetExternalIP() error = %v", err)
				return
			}
		})
	}
}
