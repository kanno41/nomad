package disconnectedclients

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad/e2e/e2eutil"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

const ns = ""

// typical wait times for this test package
var wait30s = &e2eutil.WaitConfig{Interval: time.Second, Retries: 30}
var wait60s = &e2eutil.WaitConfig{Interval: time.Second, Retries: 60}

type expectedAllocStatus struct {
	disconnected string
	unchanged    string
	replacement  string
}

func TestDisconnectedClients(t *testing.T) {

	nomad := e2eutil.NomadClient(t)
	e2eutil.WaitForLeader(t, nomad)
	e2eutil.WaitForNodesReady(t, nomad, 2) // needs at least 2 to test replacement

	testCases := []struct {
		name                    string
		jobFile                 string
		disconnectFn            func(string, time.Duration) (string, error)
		expectedAfterDisconnect expectedAllocStatus
		expectedAfterReconnect  expectedAllocStatus
	}{
		{
			// test that allocations on clients that are netsplit and
			// marked disconnected are replaced
			name:         "netsplit client no max disconnect",
			jobFile:      "./input/lost_simple.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "lost",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "complete",
				unchanged:    "running",
				replacement:  "running",
			},
		},

		{
			// test that allocations on clients that are netsplit and
			// marked disconnected are replaced but that the
			// replacements are rolled back after reconnection
			name:         "netsplit client with max disconnect",
			jobFile:      "./input/lost_max_disconnect.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations on clients that are shutdown and
			// marked disconnected are replaced
			name:         "shutdown client no max disconnect",
			jobFile:      "./input/lost_simple.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "lost",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "complete",
				unchanged:    "running",
				replacement:  "running",
			},
		},

		{
			// test that allocations on clients that are shutdown and
			// marked disconnected are replaced
			name:         "shutdown client with max disconnect",
			jobFile:      "./input/lost_max_disconnect.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template on clients that disconnect
			// run with stale data and reconnect as running.
			name:         "shutdown client with simple template",
			jobFile:      "./input/lost_template.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template with variables on clients
			// that disconnect run with stale data and reconnect as running.
			name:         "shutdown client with template and variables",
			jobFile:      "./input/lost_template_with_vars.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template with service discovery on clients
			// that disconnect run with stale data and reconnect as running.
			name:         "shutdown client with template and service discovery",
			jobFile:      "./input/lost_template_service_disco.nomad",
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			testID := uuid.Short()

			jobIDs := []string{}
			t.Cleanup(disconnectedClientsCleanup(t))
			t.Cleanup(e2eutil.CleanupJobsAndGC(t, &jobIDs))

			jobID := "test-disconnected-clients-" + testID

			err := e2eutil.Register(jobID, tc.jobFile)
			require.NoError(t, err)
			jobIDs = append(jobIDs, jobID)

			err = e2eutil.WaitForAllocStatusExpected(jobID, ns,
				[]string{"running", "running"})
			require.NoError(t, err, "job should be running")

			err = e2eutil.WaitForLastDeploymentStatus(jobID, ns, "successful", nil)
			require.NoError(t, err, "success", "deployment did not complete")

			// pick one alloc to make our disconnected alloc (and its node)
			allocs, err := e2eutil.AllocsForJob(jobID, ns)
			require.NoError(t, err, "could not query allocs for job")
			require.Len(t, allocs, 2, "could not find 2 allocs for job")

			disconnectedAllocID := allocs[0]["ID"]
			disconnectedNodeID := allocs[0]["Node ID"]
			unchangedAllocID := allocs[1]["ID"]

			// disconnect the node and wait for the results

			restartJobID, err := tc.disconnectFn(disconnectedNodeID, 30*time.Second)
			require.NoError(t, err, "expected agent disconnect job to register")
			jobIDs = append(jobIDs, restartJobID)

			err = e2eutil.WaitForNodeStatus(disconnectedNodeID, "disconnected", wait60s)
			require.NoError(t, err, "expected node to go down")

			require.NoError(t, waitForAllocStatusMap(
				jobID, disconnectedAllocID, unchangedAllocID, tc.expectedAfterDisconnect, wait60s))

			allocs, err = e2eutil.AllocsForJob(jobID, ns)
			require.NoError(t, err, "could not query allocs for job")
			require.Len(t, allocs, 3, "could not find 3 allocs for job")

			// wait for the reconnect and wait for the results

			err = e2eutil.WaitForNodeStatus(disconnectedNodeID, "ready", wait30s)
			require.NoError(t, err, "expected node to come back up")
			require.NoError(t, waitForAllocStatusMap(
				jobID, disconnectedAllocID, unchangedAllocID, tc.expectedAfterReconnect, wait60s))
		})
	}

}

// disconnectedClientsCleanup sets up a cleanup function to make sure
// we've waited for all the nodes to come back up between tests
func disconnectedClientsCleanup(t *testing.T) func() {
	nodeIDs := []string{}
	nodeStatuses, err := e2eutil.NodeStatusList()
	require.NoError(t, err)
	for _, nodeStatus := range nodeStatuses {
		nodeIDs = append(nodeIDs, nodeStatus["ID"])
	}
	return func() {
		nomad := e2eutil.NomadClient(t)
		t.Logf("waiting for %d nodes to become ready again", len(nodeIDs))
		e2eutil.WaitForNodesReady(t, nomad, len(nodeIDs))
	}
}

func waitForAllocStatusMap(jobID, disconnectedAllocID, unchangedAllocID string, expected expectedAllocStatus, wc *e2eutil.WaitConfig) error {
	var err error
	interval, retries := wc.OrDefault()
	testutil.WaitForResultRetries(retries, func() (bool, error) {
		time.Sleep(interval)
		allocs, err := e2eutil.AllocsForJob(jobID, ns)
		if err != nil {
			return false, err
		}

		var merr *multierror.Error

		for _, alloc := range allocs {
			switch allocID, allocStatus := alloc["ID"], alloc["Status"]; allocID {
			case disconnectedAllocID:
				if allocStatus != expected.disconnected {
					merr = multierror.Append(merr, fmt.Errorf(
						"disconnected alloc %q on node %q should be %q, got %q",
						allocID, alloc["Node ID"], expected.disconnected, allocStatus))
				}
			case unchangedAllocID:
				if allocStatus != expected.unchanged {
					merr = multierror.Append(merr, fmt.Errorf(
						"unchanged alloc %q on node %q should be %q, got %q",
						allocID, alloc["Node ID"], expected.unchanged, allocStatus))
				}
			default:
				if allocStatus != expected.replacement {
					merr = multierror.Append(merr, fmt.Errorf(
						"replacement alloc %q on node %q should be %q, got %q",
						allocID, alloc["Node ID"], expected.replacement, allocStatus))
				}
			}
		}
		if merr != nil {
			return false, merr.ErrorOrNil()
		}
		return true, nil
	}, func(e error) {
		err = e
	})

	// TODO(tgross): remove this block once this test has stabilized
	if err != nil {
		fmt.Printf("test failed, printing allocation status of all %q allocs for analysis\n", jobID)
		fmt.Println("----------------")
		allocs, _ := e2eutil.AllocsForJob(jobID, ns)
		for _, alloc := range allocs {
			out, _ := e2eutil.Command("nomad", "alloc", "status", alloc["ID"])
			fmt.Println(out)
			fmt.Println("----------------")
		}
	}

	return err
}

func TestDisconnectedClients_Templates(t *testing.T) {

	nomad := e2eutil.NomadClient(t)
	e2eutil.WaitForLeader(t, nomad)
	e2eutil.WaitForNodesReady(t, nomad, 2) // needs at least 2 to test replacement

	testCases := []struct {
		name                    string
		usesSecrets             bool
		jobFile                 string
		disconnectFn            func(string, time.Duration) (string, error)
		expectedAfterDisconnect expectedAllocStatus
		expectedAfterReconnect  expectedAllocStatus
	}{

		{
			// test that allocations that use a template on clients that disconnect
			// run with stale data and reconnect as running.
			name:         "shutdown client with simple template",
			jobFile:      "./input/lost_template.nomad",
			usesSecrets:  false,
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template with variables on clients
			// that disconnect run with stale data and reconnect as running.
			name:         "shutdown client with template and variables",
			jobFile:      "./input/lost_template_with_vars.nomad",
			usesSecrets:  false,
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template with service discovery on clients
			// that disconnect run with stale data and reconnect as running.
			name:         "shutdown client with template and service discovery",
			jobFile:      "./input/lost_template_service_disco.nomad",
			usesSecrets:  false,
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},

		{
			// test that allocations that use a template with vault secrets on clients
			// that disconnect run with stale data and reconnect as running.
			name:         "shutdown client with template and vault secrets",
			jobFile:      "./input/lost_template_vault_secrets.nomad",
			usesSecrets:  true,
			disconnectFn: e2eutil.AgentDisconnect,
			expectedAfterDisconnect: expectedAllocStatus{
				disconnected: "unknown",
				unchanged:    "running",
				replacement:  "running",
			},
			expectedAfterReconnect: expectedAllocStatus{
				disconnected: "running",
				unchanged:    "running",
				replacement:  "complete",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			testID := uuid.Short()

			if tc.usesSecrets {
				out, err := e2eutil.InitVaultForSecrets(testID, "15s")
				require.NoError(t, err, "error running InitVaultForSecrets", out)
			}

			jobIDs := []string{}
			t.Cleanup(disconnectedClientsCleanup(t))
			t.Cleanup(e2eutil.CleanupJobsAndGC(t, &jobIDs))

			jobID := "test-disconnected-clients-" + testID

			jobspecPath := tc.jobFile
			// If a jobspecFn is defined, run the function, save the contents
			// to disk, and override the tc.jobFile.
			if tc.usesSecrets {
				jobspecPath = path.Join(t.TempDir(), "jobspcec.nomad")
				raw, err := os.ReadFile(tc.jobFile)
				require.NoError(t, err, "error reading job file")
				jobspec := strings.ReplaceAll(string(raw), "TESTID", testID)
				err = os.WriteFile(jobspecPath, []byte(jobspec), 0644)
				require.NoError(t, err, "error writing jobspec file")
			}

			err := e2eutil.Register(jobID, jobspecPath)
			require.NoError(t, err)
			jobIDs = append(jobIDs, jobID)

			err = e2eutil.WaitForAllocStatusExpected(jobID, ns,
				[]string{"running", "running"})
			require.NoError(t, err, "job should be running")

			err = e2eutil.WaitForLastDeploymentStatus(jobID, ns, "successful", nil)
			require.NoError(t, err, "success", "deployment did not complete")

			// pick one alloc to make our disconnected alloc (and its node)
			allocs, err := e2eutil.AllocsForJob(jobID, ns)
			require.NoError(t, err, "could not query allocs for job")
			require.Len(t, allocs, 2, "could not find 2 allocs for job")

			disconnectedAllocID := allocs[0]["ID"]
			disconnectedNodeID := allocs[0]["Node ID"]
			unchangedAllocID := allocs[1]["ID"]

			// If test requires vault secrets, create them.
			if tc.usesSecrets {
				_, err := waitForVault(testID, disconnectedAllocID)
				require.NoError(t, err, "error running waitForVault")
			}

			// disconnect the node and wait for the results

			restartJobID, err := tc.disconnectFn(disconnectedNodeID, 30*time.Second)
			require.NoError(t, err, "expected agent disconnect job to register")
			jobIDs = append(jobIDs, restartJobID)

			err = e2eutil.WaitForNodeStatus(disconnectedNodeID, "disconnected", wait60s)
			require.NoError(t, err, "expected node to go down")

			require.NoError(t, waitForAllocStatusMap(
				jobID, disconnectedAllocID, unchangedAllocID, tc.expectedAfterDisconnect, wait60s))

			allocs, err = e2eutil.AllocsForJob(jobID, ns)
			require.NoError(t, err, "could not query allocs for job")
			require.Len(t, allocs, 3, "could not find 3 allocs for job")

			// wait for the reconnect and wait for the results

			err = e2eutil.WaitForNodeStatus(disconnectedNodeID, "ready", wait30s)
			require.NoError(t, err, "expected node to come back up")
			require.NoError(t, waitForAllocStatusMap(
				jobID, disconnectedAllocID, unchangedAllocID, tc.expectedAfterReconnect, wait60s))

			if tc.usesSecrets {
				_, err := e2eutil.DeleteVaultSecretsPolicy(testID)
				require.NoError(t, err, "error running DeleteVaultSecretPolicy")
			}
		})
	}

}

func waitForVault(testID, allocID string) (string, error) {
	secretsPath := "secrets-" + testID
	wc := &e2eutil.WaitConfig{Retries: 500}

	renderedCert, err := e2eutil.WaitForVaultAllocSecret(allocID, "task", "/secrets/certificate.crt",
		func(out string) bool {
			return strings.Contains(out, "BEGIN CERTIFICATE")
		}, wc)
	if err != nil {
		return renderedCert, err
	}

	out, err := e2eutil.WaitForVaultAllocSecret(allocID, "task", "/secrets/access.key",
		func(out string) bool {
			return strings.Contains(out, testID)
		}, wc)
	if err != nil {
		return out, err
	}

	var re = regexp.MustCompile(`VAULT_TOKEN=(.*)`)

	// check vault token was written and save it for later comparison
	out, err = e2eutil.AllocExec(allocID, "task", "env", ns, nil)
	if err != nil {
		return out, err
	}

	match := re.FindStringSubmatch(out)
	if match == nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	taskToken := match[1]

	// Update secret
	out, err = e2eutil.Command("vault", "kv", "put",
		fmt.Sprintf("%s/myapp", secretsPath), "key=UPDATED")
	if err != nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	// tokens will not be updated
	out, err = e2eutil.AllocExec(allocID, "task", "env", ns, nil)
	if err != nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	match = re.FindStringSubmatch(out)
	if match == nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	if taskToken != match[1] {
		return match[1], fmt.Errorf("error retrieving token: expected %s got %s", taskToken, match[1])
	}

	// cert will be renewed
	out, err = e2eutil.WaitForVaultAllocSecret(allocID, "task", "/secrets/certificate.crt",
		func(out string) bool {
			return strings.Contains(out, "BEGIN CERTIFICATE") &&
				out != renderedCert
		}, wc)
	if err != nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	// secret will *not* be renewed because it doesn't have a lease to expire
	out, err = e2eutil.WaitForVaultAllocSecret(allocID, "task", "/secrets/access.key",
		func(out string) bool {
			return strings.Contains(out, testID)
		}, wc)
	if err != nil {
		return out, fmt.Errorf("could not find VAULT_TOKEN, got:%v\n", out)
	}

	return "", nil
}
