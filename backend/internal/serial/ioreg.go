package serial

import (
	"fmt"
	"strconv"
	"strings"
)

// parseIoreg reads `ioreg -r -c IOUSBHostDevice -l` and returns, per callout
// device (/dev/cu.*), the USB device it hangs off: the nearest
// IOUSBHostDevice above the IOSerialBSDClient in the tree.
func parseIoreg(out string) map[string]Info {
	type node struct {
		depth int
		class string
		props map[string]string
	}
	var stack []*node
	res := map[string]Info{}
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "+-o "); i >= 0 {
			for len(stack) > 0 && stack[len(stack)-1].depth >= i {
				stack = stack[:len(stack)-1]
			}
			class := ""
			if c := strings.Index(line, "<class "); c >= 0 {
				class = line[c+len("<class "):]
				if e := strings.IndexAny(class, ",>"); e >= 0 {
					class = class[:e]
				}
			}
			stack = append(stack, &node{depth: i, class: class, props: map[string]string{}})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		k, v, ok := ioregProp(line)
		if !ok {
			continue
		}
		top := stack[len(stack)-1]
		top.props[k] = v
		if k != "IOCalloutDevice" {
			continue
		}
		for j := len(stack) - 1; j >= 0; j-- {
			n := stack[j]
			if n.class != "IOUSBHostDevice" && n.class != "IOUSBDevice" {
				continue
			}
			info := Info{Path: v, VID: decHex(n.props["idVendor"]), PID: decHex(n.props["idProduct"]), Serial: n.props["USB Serial Number"]}
			info.Product = n.props["USB Product Name"]
			if info.Product == "" {
				info.Product = n.props["kUSBProductString"]
			}
			res[v] = info
			break
		}
	}
	return res
}

// ioregProp parses `|   "key" = value` (value without quotes when quoted).
func ioregProp(line string) (string, string, bool) {
	s := strings.TrimLeft(line, " |")
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	end := strings.Index(s[1:], `"`)
	if end < 0 {
		return "", "", false
	}
	key := s[1 : 1+end]
	rest := strings.TrimSpace(s[2+end:])
	if !strings.HasPrefix(rest, "=") {
		return "", "", false
	}
	v := strings.TrimSpace(rest[1:])
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return key, v, true
}

// decHex turns ioreg's decimal ids ("12346") into hex ("303a").
func decHex(s string) string {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%04x", n)
}
