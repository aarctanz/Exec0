package middleware

import (
	"net"
	"net/http"
)

// IPAllowlist restricts access to the listed IPs/CIDRs.
// Requests from unlisted IPs get 403. An empty list allows all.
func IPAllowlist(allowed []string) func(http.Handler) http.Handler {
	nets := make([]*net.IPNet, 0, len(allowed))
	ips := make([]net.IP, 0, len(allowed))

	for _, entry := range allowed {
		_, cidr, err := net.ParseCIDR(entry)
		if err == nil {
			nets = append(nets, cidr)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			ips = append(ips, ip)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			clientIP := net.ParseIP(host)
			if clientIP == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			for _, ip := range ips {
				if ip.Equal(clientIP) {
					next.ServeHTTP(w, r)
					return
				}
			}
			for _, cidr := range nets {
				if cidr.Contains(clientIP) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
