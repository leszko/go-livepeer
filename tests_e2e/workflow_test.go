package tests_e2e

import (
	"context"
	"fmt"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/livepeer/go-livepeer/core"
	"github.com/livepeer/go-livepeer/eth"
	"github.com/livepeer/go-livepeer/server"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

type gethContainer struct {
	testcontainers.Container
	URI string
}

func setupGeth(ctx context.Context, t *testing.T) *gethContainer {
	req := testcontainers.ContainerRequest{
		Image:        "livepeer/geth-with-livepeer-protocol:streamflow",
		ExposedPorts: []string{"8546/tcp", "8545/tcp"},
		// TODO: Add geth health check
		//WaitingFor:   wait.ForHTTP("/"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
		return nil
	}

	ip, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
		return nil
	}

	mappedPort, err := container.MappedPort(ctx, "8546")
	if err != nil {
		t.Fatal(err)
		return nil
	}

	uri := fmt.Sprintf("http://%s:%s", ip, mappedPort.Port())

	return &gethContainer{Container: container, URI: uri}
}

func TestCompleteStreamingWorkflow(t *testing.T) {
	// given
	gethC := setupGeth(context.TODO(), t)
	defer gethC.Terminate(context.TODO())
	ethClient := newEthClient(gethC.URI, t)

	orch := startOrchestrator(ethClient)
	registerOrchestrator()
	startBroadcaster(ethClient)
	fundDepositAndReserve()

	// when
	startStream(orch)

	// then
	checkStreamIsTranscoded()
	checkTicketIsReceivedByOrchestrator()
	checkTicketIsRedeemed()
}

func newEthClient(ethUrl string, t *testing.T) eth.LivepeerEthClient {
	client, err := ethclient.Dial(ethUrl)
	if err != nil {
		t.Fatal(err)
		return nil
	}
	// TODO fill the structure
	ethCfg := eth.LivepeerEthClientConfig{
		AccountManager:     nil,
		ControllerAddr:     ethcommon.Address{},
		EthClient:          client,
		GasPriceMonitor:    nil,
		TransactionManager: nil,
		Signer:             types.LatestSignerForChainID(nil),
	}

	livepeerEthClient, err := eth.NewClient(ethCfg)
	if err != nil {
		t.Fatal(err)
		return nil
	}
	return livepeerEthClient

}

func startOrchestrator(ethClient eth.LivepeerEthClient) *server.LivepeerServer {
	n, _ := core.NewLivepeerNode(ethClient, "./tmp", nil)
	s, _ := server.NewLivepeerServer("127.0.0.1:1938", n, true, "")
	// TODO set up and start server in the Orchestrator mode
	return s
}

func registerOrchestrator() {
	// TODO
}

func startBroadcaster(ethClient eth.LivepeerEthClient) *server.LivepeerServer {
	// TODO: Start Livepeer in the Broadcaster mode
	return nil
}

func fundDepositAndReserve() {
	// TODO
}

func startStream(orch *server.LivepeerServer) {
	// TODO
}

func checkStreamIsTranscoded() {
	// TODO
}

func checkTicketIsReceivedByOrchestrator() {
	// TODO
}

func checkTicketIsRedeemed() {
	// TODO
}
