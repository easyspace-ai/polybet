package risksvc

import (
	"context"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
)

const gammaMetaTTL = 8 * time.Minute

type gammaMetaCache struct {
	at   time.Time
	disp gammaclient.TokenMarketDisplay
}

func dedupeTrimTokens(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// gammaMetaBatch returns Gamma market display fields per CLOB token id, with a small in-memory TTL cache.
func (s *Service) gammaMetaBatch(ctx context.Context, tokenIDs []string) map[string]gammaclient.TokenMarketDisplay {
	uniq := dedupeTrimTokens(tokenIDs)
	res := make(map[string]gammaclient.TokenMarketDisplay, len(uniq))
	var need []string

	s.gammaMetaMu.Lock()
	if s.gammaMeta == nil {
		s.gammaMeta = make(map[string]gammaMetaCache)
	}
	now := time.Now()
	for _, t := range uniq {
		ent, ok := s.gammaMeta[t]
		if ok && now.Sub(ent.at) < gammaMetaTTL {
			res[t] = ent.disp
		} else {
			need = append(need, t)
		}
	}
	s.gammaMetaMu.Unlock()

	if len(need) == 0 {
		return res
	}

	fetched, err := gammaclient.FetchMarketsByCLOBTokenIDs(ctx, s.cfg.HTTPPlatformProxy, need)
	if err != nil && s.log != nil {
		s.log.Debug("gamma_markets_by_token", "err", err.Error())
	}
	if fetched == nil {
		fetched = make(map[string]gammaclient.TokenMarketDisplay)
	}

	s.gammaMetaMu.Lock()
	now = time.Now()
	for _, t := range need {
		m := fetched[t]
		s.gammaMeta[t] = gammaMetaCache{at: now, disp: m}
		res[t] = m
	}
	s.gammaMetaMu.Unlock()

	return res
}
