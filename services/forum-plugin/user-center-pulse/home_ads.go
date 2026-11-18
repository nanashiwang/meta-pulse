package pulse_user_center

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// Only normalized JPEG uploads are accepted; neither images nor destinations are fetched by the server.
func (uc *UserCenter) homeAds(op string, site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		actor := ""
		var err error
		write := op == "create" || op == "update" || op == "delete"
		if write || op == "manage" || op == "preview" {
			if c.GetHeader("Authorization") == "" || (write && !communitySameOrigin(c.Request, site())) {
				communityError(c, 403, "forbidden")
				return
			}
			actor, err = answerAdminSessionUserID(c)
			if err != nil {
				communityIdentityError(c, err)
				return
			}
		}
		s, err := uc.communityEventStore(ctx)
		if err != nil {
			communityError(c, 503, "ads_unavailable")
			return
		}
		if actor != "" {
			if err = (&mysqlBindingGuard{db: s.DB}).CheckAdminIdentity(ctx, actor); err != nil {
				communityIdentityError(c, err)
				return
			}
		}
		id := c.Param("id")
		if id != "" {
			if _, e := strconv.ParseUint(id, 10, 64); e != nil {
				communityError(c, 400, "invalid_id")
				return
			}
		}
		switch op {
		case "list", "manage":
			query := "SELECT id,target,placement,revision FROM metar_home_ad WHERE enabled=TRUE ORDER BY id"
			if op == "manage" {
				query = "SELECT id,target,placement,revision,note,enabled FROM metar_home_ad ORDER BY id"
			}
			rows, e := s.DB.QueryContext(ctx, query)
			if e == nil {
				defer rows.Close()
				items := []gin.H{}
				for rows.Next() {
					var id, link, placement, revision, note string
					var enabled bool
					fields := []any{&id, &link, &placement, &revision}
					if op == "manage" {
						fields = append(fields, &note, &enabled)
					}
					if e = rows.Scan(fields...); e != nil {
						break
					}
					item := gin.H{"id": id, "link": link, "placement": placement, "revision": revision}
					if op == "manage" {
						item["note"] = note
						item["enabled"] = enabled
					}
					items = append(items, item)
				}
				if e == nil && rows.Err() == nil {
					c.JSON(200, gin.H{"code": 200, "data": items})
					return
				}
			}
		case "image", "preview":
			var pic []byte
			err = s.DB.QueryRowContext(ctx, "SELECT picture FROM metar_home_ad WHERE id=?", id).Scan(&pic)
			if errors.Is(err, sql.ErrNoRows) {
				c.Status(404)
				return
			}
			if err == nil {
				if op == "preview" {
					c.JSON(200, gin.H{"code": 200, "data": gin.H{"jpeg": pic}})
					return
				}
				c.Header("Cache-Control", "public, max-age=3600")
				writeHomeAdJPEG(c, pic)
				return
			}
		case "delete":
			_, err = s.DB.ExecContext(ctx, "DELETE FROM metar_home_ad WHERE id=?", id)
		case "create", "update":
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 900<<10)
			if err = c.Request.ParseMultipartForm(900 << 10); err != nil {
				communityError(c, 400, "invalid_upload")
				return
			}
			defer c.Request.MultipartForm.RemoveAll()
			target := c.PostForm("link")
			u, e := url.ParseRequestURI(target)
			if e != nil || len(target) > 2048 || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.Contains(target, "\\") {
				communityError(c, 400, "invalid_link")
				return
			}
			placement := c.DefaultPostForm("placement", "side")
			if placement != "side" && placement != "top" {
				communityError(c, 400, "invalid_placement")
				return
			}
			var enabled any // A legacy client omitting this field must not resume a paused ad.
			if value, ok := c.GetPostForm("enabled"); ok {
				switch value {
				case "true":
					enabled = true
				case "false":
					enabled = false
				default:
					communityError(c, 400, "invalid_enabled")
					return
				}
			}
			note := strings.TrimSpace(c.PostForm("note"))
			if !utf8.ValidString(note) || utf8.RuneCountInString(note) > 300 {
				communityError(c, 400, "invalid_note")
				return
			}
			var pic []byte
			f, _, e := c.Request.FormFile("image")
			if e == nil {
				defer f.Close()
				pic, e = io.ReadAll(f)
				cfg, ce := jpeg.DecodeConfig(bytes.NewReader(pic))
				if e != nil || ce != nil || cfg.Width <= cfg.Height || cfg.Width > 2048 {
					communityError(c, 400, "landscape_image_required")
					return
				}
				if _, e = jpeg.Decode(bytes.NewReader(pic)); e != nil {
					communityError(c, 400, "invalid_image")
					return
				}
			} else if op == "create" || !errors.Is(e, http.ErrMissingFile) {
				communityError(c, 400, "invalid_image")
				return
			}
			if op == "create" {
				var res sql.Result
				res, err = s.DB.ExecContext(ctx, "INSERT INTO metar_home_ad(target,picture,note,placement,enabled) VALUES (?,?,?,?,COALESCE(?,TRUE))", target, pic, note, placement, enabled)
				if err == nil {
					var newID int64
					newID, err = res.LastInsertId()
					if err == nil {
						c.JSON(200, gin.H{"code": 200, "data": gin.H{"id": strconv.FormatInt(newID, 10), "revision": "1"}})
						return
					}
				}
			} else {
				rev, e := strconv.ParseUint(c.PostForm("revision"), 10, 64)
				if e != nil || rev == 0 {
					communityError(c, 400, "invalid_revision")
					return
				}
				var res sql.Result
				res, err = s.DB.ExecContext(ctx, "UPDATE metar_home_ad SET target=?,note=?,placement=?,enabled=COALESCE(?,enabled),picture=COALESCE(?,picture),revision=revision+1 WHERE id=? AND revision=?", target, note, placement, enabled, pic, id, rev)
				if err == nil {
					var n int64
					n, err = res.RowsAffected()
					if err == nil && n == 1 {
						c.JSON(200, gin.H{"code": 200, "data": gin.H{"id": id, "revision": strconv.FormatUint(rev+1, 10)}})
						return
					}
					if err == nil && n == 0 {
						communityError(c, 409, "ad_changed")
						return
					}
				}
			}
		}
		if write && err == nil {
			c.JSON(200, gin.H{"code": 200, "data": true})
			return
		}
		communityError(c, 503, "ads_unavailable")
	}
}

// Do not set the raw JPEG length: Answer may compress the response with Brotli.
func writeHomeAdJPEG(c *gin.Context, pic []byte) {
	c.Header("Content-Type", "image/jpeg")
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(pic); err != nil {
		_ = c.Error(err)
		c.Abort()
	}
}
