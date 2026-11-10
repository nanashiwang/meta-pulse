package pulse_user_center

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
)

type taxonomyOrder struct {
	Version    int64    `json:"version"`
	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`
}

func (v taxonomyOrder) valid() bool {
	if v.Version < 0 || v.Version >= 9007199254740991 {
		return false
	}
	for _, list := range [][]string{v.Categories, v.Tags} {
		if list == nil || len(list) > 10000 {
			return false
		}
		seen := map[string]bool{}
		for _, slug := range list {
			if slug == "" || len(slug) > 255 || seen[slug] {
				return false
			}
			seen[slug] = true
		}
	}
	return true
}

func taxonomyJSON(c *gin.Context, cmd *taxonomyOrder) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1048577))
	if err != nil || len(raw) > 1048576 || !uniqueExperienceJSON(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(cmd) == nil && d.Decode(new(any)) == io.EOF
}

var taxonomyDB struct {
	sync.Mutex
	db *sql.DB
}

func taxonomyDatabase(ctx context.Context) (*sql.DB, error) {
	taxonomyDB.Lock()
	defer taxonomyDB.Unlock()
	if taxonomyDB.db != nil {
		return taxonomyDB.db, nil
	}
	guard, err := NewMySQLBindingGuard(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil {
		return nil, err
	}
	db := guard.(*mysqlBindingGuard).db
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS metar_taxonomy_order (id INT PRIMARY KEY, version BIGINT NOT NULL, payload MEDIUMTEXT NOT NULL) ENGINE=InnoDB`)
	if err == nil {
		_, err = db.ExecContext(ctx, `INSERT IGNORE INTO metar_taxonomy_order VALUES (1,0,'{"categories":[],"tags":[]}')`)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	taxonomyDB.db = db
	return db, nil
}
func readTaxonomyOrder(ctx context.Context, db *sql.DB) (taxonomyOrder, error) {
	var v taxonomyOrder
	var raw string
	err := db.QueryRowContext(ctx, `SELECT version,payload FROM metar_taxonomy_order WHERE id=1`).Scan(&v.Version, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (uc *UserCenter) taxonomyOrderHandler(session func(*gin.Context) (string, error), site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Robots-Tag", "noindex")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		var cmd taxonomyOrder
		var user string
		var err error
		if session != nil {
			if c.GetHeader("Authorization") == "" || c.GetHeader("X-Metar-Request") != "1" || !communitySameOrigin(c.Request, site()) {
				communityError(c, 403, "forbidden")
				return
			}
			user, err = session(c)
			if err != nil {
				communityError(c, 403, "forbidden")
				return
			}
			if !taxonomyJSON(c, &cmd) || !cmd.valid() {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		db, err := taxonomyDatabase(ctx)
		if err != nil {
			communityError(c, 503, "taxonomy_unavailable")
			return
		}
		if session != nil {
			if err = (&mysqlBindingGuard{db: db}).CheckAdminIdentity(ctx, user); err != nil {
				communityError(c, 403, "forbidden")
				return
			}
			payload, _ := json.Marshal(struct {
				Categories []string `json:"categories"`
				Tags       []string `json:"tags"`
			}{cmd.Categories, cmd.Tags})
			result, e := db.ExecContext(ctx, `UPDATE metar_taxonomy_order SET version=version+1,payload=? WHERE id=1 AND version=?`, string(payload), cmd.Version)
			if e != nil {
				communityError(c, 503, "taxonomy_unavailable")
				return
			}
			n, e := result.RowsAffected()
			if e != nil {
				communityError(c, 503, "taxonomy_unavailable")
				return
			}
			if n == 0 {
				current, e := readTaxonomyOrder(ctx, db)
				if e != nil {
					communityError(c, 503, "taxonomy_unavailable")
					return
				}
				if current.Version != cmd.Version+1 || !reflect.DeepEqual(current.Categories, cmd.Categories) || !reflect.DeepEqual(current.Tags, cmd.Tags) {
					communityError(c, 409, "taxonomy_conflict")
					return
				}
			}
		}
		value, err := readTaxonomyOrder(ctx, db)
		if err != nil {
			communityError(c, 503, "taxonomy_unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": value})
	}
}
func (uc *UserCenter) registerTaxonomyOrder(r *gin.RouterGroup, admin bool) {
	if admin {
		r.PUT("/metar/taxonomy/order", uc.taxonomyOrderHandler(answerAdminSessionUserID, plugin.SiteURL))
	} else {
		r.GET("/metar/taxonomy/order", uc.taxonomyOrderHandler(nil, plugin.SiteURL))
	}
}
