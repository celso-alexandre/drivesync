package main

import (
	"runtime"
	"strings"
)

var targetOS = runtime.GOOS

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true,
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, tempPrefix) {
		return false
	}
	if targetOS == "windows" {
		return validWindowsName(name)
	}
	return true
}

func validWindowsName(name string) bool {
	if strings.ContainsAny(name, `<>:"\|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 {
			return false
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	return !windowsReservedNames[strings.ToUpper(strings.TrimRight(stem, " "))]
}

func foldKey(p string) string {
	if caseInsensitiveNames {
		return strings.ToLower(p)
	}
	return p
}
