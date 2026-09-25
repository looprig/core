package v1_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// v0110FixtureDigests pins every fixture published at core v0.11.0.
var v0110FixtureDigests = map[string]string{
	"agent_capability_summary":                      "bfcf0f7fdba2f544170e26887d030949dfc214355b2b7aa641a45c0a02057089",
	"command_status":                                "62255249bebb7e46f6641eaa49b59766089439846b986e97b655b902fbd2f259",
	"create_request":                                "e83218a08b2540e4b3e16c06283239d9553940ea6a995f0bc445e7d14d3bec64",
	"department_capability_summary":                 "0dac24c9e866c0e1edb31ca8953cef3763e7f02b86447589d1efdaac6e368127",
	"enduring_publication":                          "b37e5d028bb87fa93995adb90529b362dcdbd03f2f34b2b39a9809dd101aa18b",
	"ephemeral_publication":                         "80625f58fa48e92ac537211ad39beb13fffcaaf510f28447a5805f5e9946b94f",
	"error_envelope":                                "48f01d370b6aa406cb5d0c266be516e2c2eefc69ba98fd5394718bf702caed62",
	"gate_projection":                               "79d460ec2bfd24abf85538eed78c2ac467037de710a0ad2f252ab38d3c851579",
	"gate_response_request":                         "a12e62679f61a5b011583b4ceac82109a27ff4bb73418b5826dc948007d8e303",
	"hostlink_attach_request":                       "d44108c59328d8d7310bdde3c5276932619bb6801a884f704c7d3116db0ca148",
	"hostlink_bind_request":                         "4e0965edb476adb490e3a5fec0ca57f26d22c7d161a38dc9fa82ca99f9e95625",
	"hostlink_capacity_report":                      "1089fa59cd5dc37c4d51d4dbd297ded26137ae3398f4d719e2cf22766522a880",
	"hostlink_command_delivery":                     "de5d6f9c0a922632ad7988efa72d615103d0eb1847fd6a8f1dd8a1f3cd7bb3e8",
	"hostlink_drain_observation":                    "67619007db93a4a607e03099065158ace426b6165c0d08a4e428e7a5a89f98a0",
	"hostlink_drain_request":                        "5042cb0004dabc13b4df4edd593effeb7b1fe181f75a6f9704e7f4ddaf79342b",
	"hostlink_error_epoch_mismatch":                 "f2cb7bc0c48dfa70cfc6e44042e4d05d8bdae0fa9ab616b3dcb5a9ffef415826",
	"hostlink_error_runtime_mismatch":               "36fcb3b67cf479f4753eed9fdb1c43c6ab81b469f409b381b6054ee86ebac62f",
	"hostlink_registry_observation":                 "aa0916364adbe45d98fc67f91116ce9d192cdf3d69ddbb0700d082a89d59c128",
	"hostlink_unbind_request":                       "73e356f9f18ad9f13b7dc797791f172f0e135a2495639e6c34de6a6f0aca9cd1",
	"input_request":                                 "8cb0a764f194fe48e3c6682d57b14af4713bcd2f05d4a021df560767ed36b9fa",
	"interrupt_request":                             "47da9267d27e82e416de1170430a5a939b29b6509587f14ca54fed891a6d3d2e",
	"journal_event":                                 "6aabbf2f334b409b95d04e23f092ad06e5854783b8855382c117f2cd209a9909",
	"journal_tip":                                   "1ea1d4f126b3de25274e0245d64cc0fffcc51b6907767700685eb30ea8e7f965",
	"object_metadata":                               "150851f9031b44d403f2f327f83cdb279adf596a80e71269366804b1d3c15c49",
	"object_reference":                              "5f6ee86a6a4ca6e55d23350af78c3e6f589b939a99d972ab484bb746fb08844a",
	"public_gate_page":                              "87b7d8018a9086ff34074a0205be0484fe21a72091817b08deffc5b4d8018fc0",
	"public_journal_page":                           "8d168b332f5744b57b6f73fb23e05c6fef4340df6ac185ae40ee09c3736eb5e3",
	"recent_session_page":                           "9166a3d4cbb704dac33a9cbe328b3b1717509773868f80241d8f6784a3d589c2",
	"restore_request":                               "854632ec27d0ce292a8821bb7b812daa93e4b955ac83e43d4e1060675bc7cac2",
	"session_reset":                                 "92e0541563ea83f5b749e9ba06b3e7ac7e961d5036f64e78502b43f25d8cbac0",
	"session_status":                                "276752fee442b6f6adb4e2b7160337c6e2dd8430a60d482e1da7b18631c6caea",
	"session_summary":                               "f5541d7695efa5d5eeb0e8212c323adcc12ee8900b8e5fd917147c38410c2650",
	"version_negotiation_request":                   "8a7a3fb6114b5bd346ea4000e264c60f7b65c977e874ceed3e50bd68da878c74",
	"version_negotiation_response":                  "50208d78350a7a160dec59a82df1499b6ca7da33e54c5eb11c97e342118e68bb",
	"version_negotiation_response_hostlink_methods": "8bffcc9f62a81a4660db9d0856b157ce03401aa91ac9e1b61205e676f73ca205",
}

func TestV0110FixturesAreByteIdentical(t *testing.T) {
	t.Parallel()
	if len(v0110FixtureDigests) != 35 {
		t.Fatalf("pinned %d fixtures, want 35", len(v0110FixtureDigests))
	}
	for stem, want := range v0110FixtureDigests {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join(v1FixtureDir, stem+".json"))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Errorf("fixture %s changed: sha256 %s, want %s", stem, got, want)
			}
		})
	}
}
