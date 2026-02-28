package cookies

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseNetscape reads cookies from a Netscape-format cookie text file for the given domain.
// Lines starting with # are skipped, except #HttpOnly_ which sets the HttpOnly flag.
// Malformed lines are skipped with a warning log.
func ParseNetscape(filePath string, domain string) ([]Cookie, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error: cannot open Netscape cookie file: %w", err)
	}
	defer f.Close()

	now := time.Now()
	dotDomain := "." + domain
	var cookies []Cookie

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")

		// Skip empty lines
		if line == "" {
			continue
		}

		// Handle #HttpOnly_ prefix
		httpOnly := false
		if strings.HasPrefix(line, "#HttpOnly_") {
			httpOnly = true
			line = line[len("#HttpOnly_"):]
		} else if strings.HasPrefix(line, "#") {
			// Skip comment lines
			continue
		}

		// Split by tab — expect exactly 7 fields
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			log.Printf("warning: skipping malformed Netscape cookie line: %q", line)
			continue
		}

		cookieDomain := fields[0]
		// fields[1] is subdomain flag — not needed for parsing
		cookiePath := fields[2]
		secure := strings.EqualFold(fields[3], "TRUE")
		expiry, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			log.Printf("warning: skipping cookie with invalid expiry: %q", fields[4])
			continue
		}
		name := fields[5]
		value := fields[6]

		// Domain filtering
		if !matchesDomain(cookieDomain, domain, dotDomain) {
			continue
		}

		// Skip expired cookies (expiry > 0 means it has an explicit expiry)
		if expiry > 0 && time.Unix(expiry, 0).Before(now) {
			continue
		}

		cookies = append(cookies, Cookie{
			Name:     name,
			Value:    value,
			Domain:   cookieDomain,
			Path:     cookiePath,
			Expiry:   time.Unix(expiry, 0),
			Secure:   secure,
			HttpOnly: httpOnly,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error: failed to read Netscape cookie file: %w", err)
	}

	return cookies, nil
}

// matchesDomain checks if a cookie domain matches the target domain.
// Matches: exact match, dot-prefix, or subdomain wildcard.
func matchesDomain(cookieDomain, domain, dotDomain string) bool {
	if cookieDomain == domain || cookieDomain == dotDomain {
		return true
	}
	// Subdomain match: cookie domain is sub.example.com and domain is example.com
	if strings.HasSuffix(cookieDomain, dotDomain) {
		return true
	}
	return false
}
