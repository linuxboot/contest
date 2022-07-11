package server

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

var (
	flagSet  *flag.FlagSet
	flagPort *int
)

func initFlags(cmd string) {
	flagSet = flag.NewFlagSet(cmd, flag.ContinueOnError)
	flagPort = flagSet.Int("port", 8080, "Port to init the server on")
}

func intRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger())

	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "live"})
	})

	return r
}

func Main(cmd string, args []string, sigs <-chan os.Signal) error {
	initFlags(cmd)
	if err := flagSet.Parse(args); err != nil {
		return err
	}

	router := intRouter()
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *flagPort),
		Handler: router,
	}

	go func() {
		<-sigs
		log.Print("Closing the server")
		if err := server.Close(); err != nil {
			log.Fatalf("Server Close Err: %v", err)
		}
	}()

	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		// server closed under signal
		err = nil
	}
	return err
}
