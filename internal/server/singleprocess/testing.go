package singleprocess

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/boltdb/bolt"
	"github.com/imdario/mergo"
	"github.com/mitchellh/go-testing-interface"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/waypoint/internal/server"
	pb "github.com/hashicorp/waypoint/internal/server/gen"
	serverptypes "github.com/hashicorp/waypoint/internal/server/ptypes"
)

// TestServer starts a singleprocess server and returns the connected client.
// We use t.Cleanup to ensure resources are automatically cleaned up.
func TestServer(t testing.T) pb.WaypointClient {
	impl, err := New(testDB(t))
	require.NoError(t, err)
	return server.TestServer(t, impl)
}

func TestEntrypoint(t testing.T, client pb.WaypointClient) (string, string, func()) {
	instanceId, err := server.Id()
	require.NoError(t, err)

	ctx := context.Background()

	resp, err := client.UpsertDeployment(ctx, &pb.UpsertDeploymentRequest{
		Deployment: serverptypes.TestValidDeployment(t, &pb.Deployment{
			Component: &pb.Component{
				Name: "testapp",
			},
		}),
	})
	require.NoError(t, err)

	dep := resp.Deployment

	// Create the config
	stream, err := client.EntrypointConfig(ctx, &pb.EntrypointConfigRequest{
		InstanceId:   instanceId,
		DeploymentId: dep.Id,
	})
	require.NoError(t, err)

	// Wait for the first config so that we know we're registered
	_, err = stream.Recv()
	require.NoError(t, err)

	return instanceId, dep.Id, func() {
		stream.CloseSend()
	}
}

// TestRunner registers a runner and returns the ID and a function to
// deregister the runner. This uses t.Cleanup so that the runner will always
// be deregistered on test completion.
func TestRunner(t testing.T, client pb.WaypointClient, r *pb.Runner) (string, func()) {
	require := require.New(t)
	ctx := context.Background()

	// Get the runner
	if r == nil {
		r = &pb.Runner{}
	}
	id, err := server.Id()
	require.NoError(mergo.Merge(r, &pb.Runner{Id: id}))

	// Open the config stream
	stream, err := client.RunnerConfig(ctx)
	require.NoError(err)
	t.Cleanup(func() { stream.CloseSend() })

	// Register
	require.NoError(err)
	require.NoError(stream.Send(&pb.RunnerConfigRequest{
		Event: &pb.RunnerConfigRequest_Open_{
			Open: &pb.RunnerConfigRequest_Open{
				Runner: r,
			},
		},
	}))

	// Wait for first message to confirm we're registered
	_, err = stream.Recv()
	require.NoError(err)

	return id, func() { stream.CloseSend() }
}

func testDB(t testing.T) *bolt.DB {
	t.Helper()

	// Temporary directory for the database
	td, err := ioutil.TempDir("", "test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(td) })

	// Create the DB
	db, err := bolt.Open(filepath.Join(td, "test.db"), 0600, nil)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}
