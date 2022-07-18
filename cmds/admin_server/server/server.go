package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type RouteHandler struct {
	storage storage.Storage
}

// status is a simple endpoint to check if the serves is alive
func (r *RouteHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "live"})
}

// addLog inserts a new log entry inside the database
func (r *RouteHandler) addLog(c *gin.Context) {
	var log storage.Log
	if err := c.Bind(&log); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "err", "msg": "bad formatted log"})
		fmt.Fprintf(os.Stderr, "Err while binding request body %v", err)
		return
	}

	err := r.storage.StoreLog(log)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInsert):
			c.JSON(http.StatusInternalServerError, gin.H{"status": "err", "msg": "error while storing the log"})
		case errors.Is(err, storage.ErrReadOnlyStorage):
			c.JSON(http.StatusNotImplemented, gin.H{"status": "err", "msg": "not supported action"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "err", "msg": "unknown server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func initRouter(ctx xcontext.Context, rh RouteHandler) *gin.Engine {

	r := gin.New()
	r.Use(gin.Logger())

	r.GET("/status", rh.status)
	r.POST("/log", rh.addLog)

	return r
}

func Serve(ctx xcontext.Context, port int, storage storage.Storage) error {
	log := ctx.Logger()

	routeHandler := RouteHandler{
		storage: storage,
	}
	router := initRouter(ctx, routeHandler)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
	}

	go func() {
		<-ctx.Done()
		// on cancel close the server
		log.Debugf("Closing the server")
		if err := server.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing the server: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return ctx.Err()
}
