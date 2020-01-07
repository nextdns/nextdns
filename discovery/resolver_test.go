package discovery

import "testing"

func Test_isValidName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"iPhone", true},
		{"Android_1", true},
		{"331e87e5-3018-5336-23f3-595cdea48d9b", false},
		{"CC_22_3D_E4_CE_FE", false},
		{"10-0-0-213", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidName(tt.name); got != tt.want {
				t.Errorf("isValidName() = %v, want %v", got, tt.want)
			}
		})
	}
}
