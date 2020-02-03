package discovery

import "strings"

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	// ignore 331e87e5-3018-5336-23f3-595cdea48d9b
	if len(name) == 36 &&
		name[8] == '-' && name[13] == '-' && name[18] == '-' && name[23] == '-' &&
		strings.Trim(name, "0123456789abcdef-") == "" {
		return false
	}
	// ignore CC_22_3D_E4_CE_FE
	if len(name) == 17 &&
		name[2] == '_' && name[5] == '_' && name[8] == '_' && name[11] == '_' && name[14] == '_' &&
		strings.Trim(name, "0123456789ABCDEF_") == "" {
		return false
	}
	// ignore 10-0-0-213
	if len(name) >= 7 && len(name) <= 15 &&
		strings.Trim(name, "0123456789-") == "" {
		return false
	}

	return true
}

func normalizeName(name string) string {
	if idx := strings.IndexByte(name, '.'); idx != -1 {
		name = name[:idx] // remove .local. suffix
	}
	return name
}
