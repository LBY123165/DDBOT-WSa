package cfg

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/Sora233/MiraiGo-Template/config"
)

var cfgMu sync.Mutex

// WriteConfigKeyValue safely updates a single key in application.yaml.
// The key uses dot notation (e.g. "weibo.alertGroupId").
// It performs line-by-line text manipulation to preserve all other content.
func WriteConfigKeyValue(key, value string) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	cfgFile := config.GlobalConfig.ConfigFileUsed()
	if cfgFile == "" {
		cfgFile = "application.yaml"
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return err
	}

	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid config key format: %s (expected section.name)", key)
	}
	section := parts[0]
	keyName := parts[1]

	content := string(data)
	lines := strings.Split(content, "\n")

	var out []string
	inSection := false
	indentSection := ""
	inserted := false
	keyRe := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(keyName) + `:\s*`)

	for i, line := range lines {
		trim := strings.TrimSpace(line)

		// Detect section header (e.g. "weibo:")
		if strings.HasPrefix(trim, section+":") && !inSection {
			inSection = true
			idx := strings.Index(line, section+":")
			indentSection = line[:idx]
			out = append(out, line)
			continue
		}

		if inSection {
			// Exit section: non-indented non-empty line
			if len(trim) > 0 && !strings.HasPrefix(line, indentSection+" ") && !strings.HasPrefix(line, indentSection+"\t") {
				// Insert before exiting if not yet inserted
				if !inserted {
					out = append(out, fmt.Sprintf("%s  %s: %s", indentSection, keyName, value))
					inserted = true
				}
				inSection = false
			} else {
				// Inside section: check for existing key
				if m := keyRe.FindStringSubmatch(line); m != nil {
					out = append(out, fmt.Sprintf("%s  %s: %s", indentSection, keyName, value))
					inserted = true
					continue
				}
			}
		}

		out = append(out, line)

		// Last line: still in section and not inserted
		if i == len(lines)-1 && inSection && !inserted {
			out = append(out, fmt.Sprintf("%s  %s: %s", indentSection, keyName, value))
			inserted = true
		}
	}

	// Section not found: append new section
	if !inserted {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, section+":")
		out = append(out, fmt.Sprintf("  %s: %s", keyName, value))
	}

	return os.WriteFile(cfgFile, []byte(strings.Join(out, "\n")), 0o644)
}
