package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func initRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger())

	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "live"})
	})

	return r
}

func Serve(ctx xcontext.Context, port int) error {
	log := ctx.Logger()

	router := initRouter()
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
	}

	go func() {
		<-ctx.Done()
		// on cancel close the server
		log.Debugf("Closing the server")
		server.Close()
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return ctx.Err()
}
