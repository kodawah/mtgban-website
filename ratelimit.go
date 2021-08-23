package main

import (
	"fmt"
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

type visitor struct {
	*rate.Limiter
}

type rateLimiter struct {
	sync.RWMutex

	burst    int
	rate     rate.Limit
	visitors map[string]*visitor
}

const (
	APIRequestsPerSec  = 10
	UserRequestsPerSec = 3
)

var APIRateLimiter = &rateLimiter{
	rate:     APIRequestsPerSec,
	burst:    2,
	visitors: map[string]*visitor{},
}

var UserRateLimiter = &rateLimiter{
	rate:     UserRequestsPerSec,
	burst:    1,
	visitors: map[string]*visitor{},
}

// Allow checks if given ip has not exceeded rate limit
func (l *rateLimiter) allow(ip string) bool {
	l.RLock()
	v, exists := l.visitors[ip]
	l.RUnlock()

	if !exists {
		v = &visitor{
			Limiter: rate.NewLimiter(l.rate, l.burst),
		}
		l.Lock()
		l.visitors[ip] = v
		l.Unlock()
	}

	return v.Allow()
}

func IpAddress(r *http.Request) (net.IP, error) {
	addr := r.RemoteAddr

	xReal := r.Header.Get("X-Real-Ip")
	xForwarded := r.Header.Get("X-Forwarded-For")
	if xReal != "" {
		addr = xReal
	} else if xForwarded != "" {
		addr = xForwarded
	}

	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("addr: %q is not IP:port", addr)
	}

	userIP := net.ParseIP(ip)
	if userIP == nil {
		return nil, fmt.Errorf("ip: %q is not a valid IP address", ip)
	}

	return userIP, nil
}
