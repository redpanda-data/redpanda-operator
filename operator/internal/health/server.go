package health

import "net/http"

type HealthChecker struct{}

func (c *HealthChecker) HandleRequest(req *http.Request) error {
	return nil
}
