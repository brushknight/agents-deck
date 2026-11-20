package serial

import "strings"

// parseWinHardwareID reads the USB ids out of a Windows device key name:
// USB\VID_303A&PID_1001&MI_00 or FTDIBUS\VID_0403+PID_6001+A50285BIA.
// serial is the part after the ids for FTDI, where the driver puts it.
func parseWinHardwareID(key string) (vid, pid, serial string) {
	parts := strings.FieldsFunc(strings.ToUpper(key), func(r rune) bool { return r == '&' || r == '+' || r == '\\' })
	orig := strings.FieldsFunc(key, func(r rune) bool { return r == '&' || r == '+' || r == '\\' })
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "VID_") && len(p) == 8:
			vid = strings.ToLower(p[4:])
		case strings.HasPrefix(p, "PID_") && len(p) == 8:
			pid = strings.ToLower(p[4:])
			if strings.Contains(key, "+") && i+1 < len(orig) {
				serial = orig[i+1]
			}
		}
	}
	return vid, pid, serial
}

// winInstanceSerial: a USB instance id is the device's serial number unless
// Windows made it up (those contain '&').
func winInstanceSerial(instance string) string {
	if strings.Contains(instance, "&") {
		return ""
	}
	return instance
}

// winFriendly drops the " (COM5)" Windows appends to a friendly name.
func winFriendly(name, port string) string {
	return strings.TrimSpace(strings.TrimSuffix(name, "("+port+")"))
}
