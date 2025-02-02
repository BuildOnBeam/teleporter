package ictt

import (
	"context"
	"math/big"

	nativetokenhome "github.com/ava-labs/teleporter/abi-bindings/go/ictt/TokenHome/NativeTokenHome"
	localnetwork "github.com/ava-labs/teleporter/tests/network"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	. "github.com/onsi/gomega"
)

/*
*
  - Deploy a NativeTokenHome on the primary network
  - Deploys NativeTokenRemote to Subnet A and Subnet B
  - Transfers native tokens from the C-Chain to Subnet A as Subnet A's native token
  - Transfers native tokens from the C-Chain to Subnet B as Subnet B's native token
    to collateralize the Subnet B token transferrer
  - Transfer tokens from Subnet A to Subnet B through multi-hop
  - Transfer back tokens from Subnet B to Subnet A through multi-hop
*/
func NativeTokenHomeNativeTokenRemoteMultiHop(network *localnetwork.LocalNetwork, teleporter utils.TeleporterTestInfo) {
	cChainInfo := network.GetPrimaryNetworkInfo()
	subnetAInfo, subnetBInfo := network.GetTwoSubnets()
	fundedAddress, fundedKey := network.GetFundedAccountInfo()

	ctx := context.Background()

	// decimalsShift is always 0 for native to native
	decimalsShift := uint8(0)

	// Deploy an example WAVAX on the primary network
	wavaxAddress, wavax := utils.DeployWrappedNativeToken(
		ctx,
		fundedKey,
		cChainInfo,
		"AVAX",
	)

	// Create a NativeTokenHome on the primary network
	nativeTokenHomeAddress, nativeTokenHome := utils.DeployNativeTokenHome(
		ctx,
		teleporter,
		fundedKey,
		cChainInfo,
		fundedAddress,
		wavaxAddress,
	)

	// Deploy a NativeTokenRemote to Subnet A
	nativeTokenRemoteAddressA, nativeTokenRemoteA := utils.DeployNativeTokenRemote(
		ctx,
		teleporter,
		subnetAInfo,
		"SUBA",
		fundedAddress,
		cChainInfo.BlockchainID,
		nativeTokenHomeAddress,
		utils.NativeTokenDecimals,
		initialReserveImbalance,
		burnedFeesReportingRewardPercentage,
	)

	// Deploy a NativeTokenRemote to Subnet B
	nativeTokenRemoteAddressB, nativeTokenRemoteB := utils.DeployNativeTokenRemote(
		ctx,
		teleporter,
		subnetBInfo,
		"SUBB",
		fundedAddress,
		cChainInfo.BlockchainID,
		nativeTokenHomeAddress,
		utils.NativeTokenDecimals,
		initialReserveImbalance,
		burnedFeesReportingRewardPercentage,
	)

	// Register both NativeTokenDestinations on the NativeTokenHome
	collateralAmountA := utils.RegisterTokenRemoteOnHome(
		ctx,
		teleporter,
		cChainInfo,
		nativeTokenHomeAddress,
		subnetAInfo,
		nativeTokenRemoteAddressA,
		initialReserveImbalance,
		utils.GetTokenMultiplier(decimalsShift),
		multiplyOnRemote,
		fundedKey,
	)

	collateralAmountB := utils.RegisterTokenRemoteOnHome(
		ctx,
		teleporter,
		cChainInfo,
		nativeTokenHomeAddress,
		subnetBInfo,
		nativeTokenRemoteAddressB,
		initialReserveImbalance,
		utils.GetTokenMultiplier(decimalsShift),
		multiplyOnRemote,
		fundedKey,
	)

	// Add collateral for both NativeTokenDestinations
	utils.AddCollateralToNativeTokenHome(
		ctx,
		cChainInfo,
		nativeTokenHome,
		nativeTokenHomeAddress,
		subnetAInfo.BlockchainID,
		nativeTokenRemoteAddressA,
		collateralAmountA,
		fundedKey,
	)

	utils.AddCollateralToNativeTokenHome(
		ctx,
		cChainInfo,
		nativeTokenHome,
		nativeTokenHomeAddress,
		subnetBInfo.BlockchainID,
		nativeTokenRemoteAddressB,
		collateralAmountB,
		fundedKey,
	)

	// Generate new recipient to receive transferred tokens
	recipientKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	recipientAddress := crypto.PubkeyToAddress(recipientKey.PublicKey)

	amount := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10))

	// Send tokens from C-Chain to Subnet A
	inputA := nativetokenhome.SendTokensInput{
		DestinationBlockchainID:            subnetAInfo.BlockchainID,
		DestinationTokenTransferrerAddress: nativeTokenRemoteAddressA,
		Recipient:                          recipientAddress,
		PrimaryFeeTokenAddress:             wavaxAddress,
		PrimaryFee:                         big.NewInt(1e18),
		SecondaryFee:                       big.NewInt(0),
		RequiredGasLimit:                   utils.DefaultNativeTokenRequiredGas,
	}

	receipt, transferredAmountA := utils.SendNativeTokenHome(
		ctx,
		cChainInfo,
		nativeTokenHome,
		nativeTokenHomeAddress,
		wavax,
		inputA,
		amount,
		fundedKey,
	)

	// Relay the message to subnet A and check for a native token mint withdrawal
	teleporter.RelayTeleporterMessage(
		ctx,
		receipt,
		cChainInfo,
		subnetAInfo,
		true,
		fundedKey,
	)

	// Verify the recipient received the tokens
	utils.CheckBalance(ctx, recipientAddress, transferredAmountA, subnetAInfo.RPCClient)

	// Send tokens from C-Chain to Subnet B
	inputB := nativetokenhome.SendTokensInput{
		DestinationBlockchainID:            subnetBInfo.BlockchainID,
		DestinationTokenTransferrerAddress: nativeTokenRemoteAddressB,
		Recipient:                          recipientAddress,
		PrimaryFeeTokenAddress:             wavaxAddress,
		PrimaryFee:                         big.NewInt(1e18),
		SecondaryFee:                       big.NewInt(0),
		RequiredGasLimit:                   utils.DefaultNativeTokenRequiredGas,
	}
	receipt, transferredAmountB := utils.SendNativeTokenHome(
		ctx,
		cChainInfo,
		nativeTokenHome,
		nativeTokenHomeAddress,
		wavax,
		inputB,
		amount,
		fundedKey,
	)

	// Relay the message to subnet B and check for a native token mint withdrawal
	teleporter.RelayTeleporterMessage(
		ctx,
		receipt,
		cChainInfo,
		subnetBInfo,
		true,
		fundedKey,
	)

	// Verify the recipient received the tokens
	utils.CheckBalance(ctx, recipientAddress, transferredAmountB, subnetBInfo.RPCClient)

	// Multi-hop transfer to Subnet B
	// Send half of the received amount to account for gas expenses
	amountToSendA := new(big.Int).Div(transferredAmountA, big.NewInt(2))

	utils.SendNativeMultiHopAndVerify(
		ctx,
		teleporter,
		fundedKey,
		recipientAddress,
		subnetAInfo,
		nativeTokenRemoteA,
		nativeTokenRemoteAddressA,
		subnetBInfo,
		nativeTokenRemoteB,
		nativeTokenRemoteAddressB,
		cChainInfo,
		amountToSendA,
		big.NewInt(0),
	)

	// Again, send half of the received amount to account for gas expenses
	amountToSendB := new(big.Int).Div(amountToSendA, big.NewInt(2))
	secondaryFeeAmount := new(big.Int).Div(amountToSendB, big.NewInt(4))

	// Multi-hop transfer back to Subnet A
	utils.SendNativeMultiHopAndVerify(
		ctx,
		teleporter,
		fundedKey,
		recipientAddress,
		subnetBInfo,
		nativeTokenRemoteB,
		nativeTokenRemoteAddressB,
		subnetAInfo,
		nativeTokenRemoteA,
		nativeTokenRemoteAddressA,
		cChainInfo,
		amountToSendB,
		secondaryFeeAmount,
	)
}
