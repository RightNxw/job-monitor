package proxy

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
)

type Pool struct {
	mu      sync.Mutex
	proxies []string
}

func NewPool(filePath string) *Pool {
	p := &Pool{}
	if filePath != "" {
		p.loadFromFile(filePath)
	}
	return p
}

func (p *Pool) loadFromFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("[proxy] could not open %s: %v (running without proxies)", path, err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p.proxies = append(p.proxies, normalizeProxy(line))
	}
	log.Printf("[proxy] loaded %d proxies from %s", len(p.proxies), path)
}

// normalizeProxy converts host:port:user:pass format to http://user:pass@host:port URL.
func normalizeProxy(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "socks5://") {
		return raw
	}
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) == 4 {
		return fmt.Sprintf("http://%s:%s@%s:%s", parts[2], parts[3], parts[0], parts[1])
	}
	if len(parts) == 2 {
		return fmt.Sprintf("http://%s:%s", parts[0], parts[1])
	}
	return raw
}

// Get returns a random proxy URL, or empty string if none available.
func (p *Pool) Get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.proxies) == 0 {
		return ""
	}
	return p.proxies[rand.Intn(len(p.proxies))]
}

// Count returns how many proxies are in the pool.
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.proxies)
}
