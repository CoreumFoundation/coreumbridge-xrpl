//nolint:godot
package keys

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cometbft/cometbft/libs/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func bech32Prefixes(config *sdk.Config) []string {
	return []string{
		config.GetBech32AccountAddrPrefix(),
		config.GetBech32AccountPubPrefix(),
		config.GetBech32ValidatorAddrPrefix(),
		config.GetBech32ValidatorPubPrefix(),
		config.GetBech32ConsensusAddrPrefix(),
		config.GetBech32ConsensusPubPrefix(),
	}
}

type hexOutput struct {
	Human string `json:"human"`
	Bytes string `json:"bytes"`
}

func (ho hexOutput) String() string {
	return fmt.Sprintf("Human readable part: %v\nBytes (hex): %s", ho.Human, ho.Bytes)
}

func newHexOutput(human string, bs []byte) hexOutput {
	return hexOutput{Human: human, Bytes: fmt.Sprintf("%X", bs)}
}

type bech32Output struct {
	Formats []string `json:"formats"`
}

func newBech32Output(config *sdk.Config, bs []byte) bech32Output {
	bech32Prefixes := bech32Prefixes(config)
	out := bech32Output{Formats: make([]string, len(bech32Prefixes))}

	for i, prefix := range bech32Prefixes {
		bech32Addr, err := bech32.ConvertAndEncode(prefix, bs)
		if err != nil {
			panic(err)
		}

		out.Formats[i] = bech32Addr
	}

	return out
}

func (bo bech32Output) String() string {
	out := make([]string, len(bo.Formats))

	for i, format := range bo.Formats {
		out[i] = "  - " + format
	}

	return "Bech32 Formats:\n" + strings.Join(out, "\n")
}

// ParseKeyStringCommand parses an address from hex to bech32 and vice versa.
func ParseKeyStringCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse <hex-or-bech32-address>",
		Short: "Parse address from hex to bech32 and vice versa",
		Long: `Convert and print to stdout key addresses and fingerprints from
hexadecimal into bech32 cosmos prefixed format and vice versa.
`,
		Args: cobra.ExactArgs(1),
		RunE: parseKey,
	}

	return cmd
}

func parseKey(cmd *cobra.Command, args []string) error {
	config, _ := sdk.GetSealedConfig(cmd.Context())
	return doParseKey(cmd, config, args)
}

func doParseKey(cmd *cobra.Command, config *sdk.Config, args []string) error {
	addr := strings.TrimSpace(args[0])
	outstream := cmd.OutOrStdout()

	if len(addr) == 0 {
		return errors.New("couldn't parse empty input")
	}

	output, _ := cmd.Flags().GetString(cli.OutputFlag)
	if !runFromBech32(outstream, addr, output) && !runFromHex(config, outstream, addr, output) {
		return errors.New("couldn't find valid bech32 nor hex data")
	}

	return nil
}

// print info from bech32
func runFromBech32(w io.Writer, bech32str, output string) bool {
	hrp, bz, err := bech32.DecodeAndConvert(bech32str)
	if err != nil {
		return false
	}

	displayParseKeyInfo(w, newHexOutput(hrp, bz), output)

	return true
}

// print info from hex
func runFromHex(config *sdk.Config, w io.Writer, hexstr, output string) bool {
	bz, err := hex.DecodeString(hexstr)
	if err != nil {
		return false
	}

	displayParseKeyInfo(w, newBech32Output(config, bz), output)

	return true
}

func displayParseKeyInfo(w io.Writer, stringer fmt.Stringer, output string) {
	var (
		err error
		out []byte
	)

	switch output {
	case OutputFormatText:
		out, err = yaml.Marshal(&stringer)

	case OutputFormatJSON:
		out, err = json.Marshal(&stringer)
	}

	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintln(w, string(out))
}
