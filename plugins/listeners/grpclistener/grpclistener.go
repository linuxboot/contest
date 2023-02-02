package grpclistener

import (
	context "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	grpcreflect "github.com/bufbuild/connect-grpcreflect-go"
	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	contestlistener "github.com/linuxboot/contest/plugins/listeners/grpclistener/gen/contest/v1"
	"github.com/linuxboot/contest/plugins/listeners/grpclistener/gen/contest/v1/contestlistenerconnect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/bufbuild/connect-go"
)

type Endpoint struct {
	buffer io.Reader
}

type GRPCListener struct{}

type GRPCServer struct {
	ctx       xcontext.Context
	api       *api.API
	Endpoints map[int]*Endpoint
}

func New() *GRPCListener {
	return &GRPCListener{}
}

func (grpcl *GRPCListener) Serve(ctx xcontext.Context, a *api.API) error {
	ctx.Infof("Starting GRPCListener...\n")

	mux := http.NewServeMux()

	// Add reflection API
	reflector := grpcreflect.NewStaticReflector(
		"contest.v1.ConTestService",
	)

	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// Add our gRPC Service
	mux.Handle(contestlistenerconnect.NewConTestServiceHandler(&GRPCServer{
		ctx:       ctx,
		api:       a,
		Endpoints: make(map[int]*Endpoint),
	}))

	err := http.ListenAndServe(
		":8080",
		h2c.NewHandler(mux, &http2.Server{}),
	)

	return err
}

func (s *GRPCServer) StartJob(ctx context.Context, req *connect.Request[contestlistener.StartJobRequest]) (*connect.Response[contestlistener.StartJobResponse], error) {
	if req.Msg.Job == nil {
		s.ctx.Errorf("Job is nil")
		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), fmt.Errorf("Job is nil")
	}

	if req.Msg.Requestor == "" {
		s.ctx.Errorf("Requestor is not set")

		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), fmt.Errorf("Requestor is not set")
	}

	resp, err := s.api.Start(s.ctx, api.EventRequestor(req.Msg.Requestor), string(req.Msg.Job))
	if err != nil {
		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), err
	}

	var r api.ResponseDataStart
	if api.ResponseTypeToName[resp.Type] == "ResponseTypeStart" {
		r = resp.Data.(api.ResponseDataStart)
	}

	s.Endpoints[int(r.JobID)] = &Endpoint{}

	return connect.NewResponse(&contestlistener.StartJobResponse{
		JobId: int32(r.JobID),
		Error: "",
	}), nil
}

func (s *GRPCServer) StatusJob(ctx context.Context, req *connect.Request[contestlistener.StatusJobRequest], resp *connect.ServerStream[contestlistener.StatusJobResponse]) error {
	if req.Msg.Requestor == "" {
		s.ctx.Errorf("Requestor is not set")

		return fmt.Errorf("Requestor is not set")
	}

	if s.Endpoints[int(req.Msg.JobId)] == nil {
		s.ctx.Errorf("JobID does not exist.")

		return fmt.Errorf("JobID does not exist.")
	}

	apiResp, err := s.api.Status(s.ctx, api.EventRequestor(req.Msg.Requestor), types.JobID(req.Msg.JobId))
	if err != nil {
		s.ctx.Errorf("api.Status() = '%v'", err)

		return err
	}

	var r api.ResponseDataStatus
	if api.ResponseTypeToName[apiResp.Type] == "ResponseTypeStatus" {
		r = apiResp.Data.(api.ResponseDataStatus)
	}

	if r.Status == nil {
		s.ctx.Errorf("api.Status(): Returned job.Status == nil")

		return fmt.Errorf("api.Status(): Returned job.Status == nil")
	}

	reportBytes, err := json.Marshal(r.Status)
	if err != nil {
		s.ctx.Errorf("Unable to Marshal Status")

		return fmt.Errorf("Unable to Marshal Status")
	}

	newResp := &contestlistener.StatusJobResponse{
		Status: r.Status.State,
		Error:  r.Status.StateErrMsg,
		Report: reportBytes,
	}
	err = resp.Send(newResp)

	return err
}
