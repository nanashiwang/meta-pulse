package transporthttp

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
)

// bindStrictJSON decodes exactly one JSON value. Gin's default JSON binder
// accepts a valid first value and silently leaves a second value unread, which
// would make a signed mutation's authenticated bytes differ from its semantic
// request. The signature verifier rejects this before nonce claim in production;
// this helper keeps the route boundary strict as well when used standalone.
func bindStrictJSON(c *gin.Context, target any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return errors.New("request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
