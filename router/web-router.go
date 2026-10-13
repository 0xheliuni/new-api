package router

import (
	"embed"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded frontend assets for both themes.
type WebAssets struct {
	BuildFS          embed.FS
	IndexPage        []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

// useClassicTheme returns true when the THEME environment variable is "classic".
func useClassicTheme() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("THEME")), "classic")
}

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	compress := gzip.Gzip(gzip.DefaultCompression)

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		func(c *gin.Context) {
			if !frontendFS.Exists("/", c.Request.URL.Path) && !classicFS.Exists("/", c.Request.URL.Path) {
				compress(c)
			}
		},
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(frontendFS),
		middleware.Cache(),
		func(c *gin.Context) {
			if useClassicTheme() {
				middleware.ServeFrontendFiles(classicFS)(c)
			} else {
				middleware.ServeFrontendFiles(frontendFS)(c)
			}
		},
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
				controller.RelayNotFound(c)
				return
			}
			c.Header("Cache-Control", "no-cache")
			if useClassicTheme() {
				c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
			} else {
				c.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
			}
		},
	)
}
