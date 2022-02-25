package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

type gethContainer struct {
	testcontainers.Container
	URI string
}

func setupGeth(ctx context.Context) (*gethContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "leszko/geth-with-livepeer-protocol:streamflow",
		ExposedPorts: []string{"8546/tcp", "8545/tcp"},
		// TODO: Add geth health check
		//WaitingFor:   wait.ForHTTP("/"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	ip, err := container.Host(ctx)
	if err != nil {
		return nil, err
	}

	mappedPort, err := container.MappedPort(ctx, "8546")
	if err != nil {
		return nil, err
	}

	uri := fmt.Sprintf("http://%s:%s", ip, mappedPort.Port())

	return &gethContainer{Container: container, URI: uri}, nil
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	gethC, err := setupGeth(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Clean up the container after the test is complete
	defer gethC.Terminate(ctx)

	//resp, err := http.Get(gethC.URI)
	//if resp.StatusCode != http.StatusOK {
	//	t.Fatalf("Expected status code %d. Got %d.", http.StatusOK, resp.StatusCode)
	//}
}
