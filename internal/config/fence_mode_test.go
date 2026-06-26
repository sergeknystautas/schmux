package config

import "testing"

func TestGetFenceMode(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{"empty defaults to optional_off", "", FenceModeOptionalOff},
		{"unknown defaults to optional_off", "bogus", FenceModeOptionalOff},
		{"disabled preserved", FenceModeDisabled, FenceModeDisabled},
		{"optional_off preserved", FenceModeOptionalOff, FenceModeOptionalOff},
		{"optional_on preserved", FenceModeOptionalOn, FenceModeOptionalOn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{ConfigData: ConfigData{FenceMode: tt.set}}
			if got := c.GetFenceMode(); got != tt.want {
				t.Errorf("GetFenceMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
