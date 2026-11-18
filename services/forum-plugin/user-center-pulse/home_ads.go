package pulse_user_center

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
		if op == "create" || op == "delete" {
			if c.GetHeader("Authorization") == "" || !communitySameOrigin(c.Request, site()) {
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
		case "list":
			rows, e := s.DB.QueryContext(ctx, "SELECT id,target FROM metar_home_ad ORDER BY id")
			if e == nil {
				defer rows.Close()
				items := []gin.H{}
				for rows.Next() {
					var id, link string
					if e = rows.Scan(&id, &link); e != nil {
						break
					}
					items = append(items, gin.H{"id": id, "link": link})
				}
				if e == nil && rows.Err() == nil {
					c.JSON(200, gin.H{"code": 200, "data": items})
					return
				}
			}
		case "image":
			var pic []byte
			err = s.DB.QueryRowContext(ctx, "SELECT picture FROM metar_home_ad WHERE id=?", id).Scan(&pic)
			if errors.Is(err, sql.ErrNoRows) {
				c.Status(404)
				return
			}
			if err == nil {
				c.Header("Cache-Control", "public, max-age=3600")
				c.Data(200, "image/jpeg", pic)
				return
			}
		case "delete":
			_, err = s.DB.ExecContext(ctx, "DELETE FROM metar_home_ad WHERE id=?", id)
		case "create":
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
			f, _, e := c.Request.FormFile("image")
			if e != nil {
				communityError(c, 400, "invalid_image")
				return
			}
			defer f.Close()
			pic, e := io.ReadAll(f)
			cfg, ce := jpeg.DecodeConfig(bytes.NewReader(pic))
			if e != nil || ce != nil || cfg.Width <= cfg.Height || cfg.Width > 2048 {
				communityError(c, 400, "landscape_image_required")
				return
			}
			if _, e = jpeg.Decode(bytes.NewReader(pic)); e != nil {
				communityError(c, 400, "invalid_image")
				return
			}
			_, err = s.DB.ExecContext(ctx, "INSERT INTO metar_home_ad(target,picture) VALUES (?,?)", target, pic)
		}
		if (op == "create" || op == "delete") && err == nil {
			c.JSON(200, gin.H{"code": 200, "data": true})
			return
		}
		communityError(c, 503, "ads_unavailable")
	}
}
