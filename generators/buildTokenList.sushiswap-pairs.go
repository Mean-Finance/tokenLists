package main

import (
	"context"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/migratooor/tokenLists/generators/common/contracts"
	"github.com/migratooor/tokenLists/generators/common/ethereum"
	"github.com/migratooor/tokenLists/generators/common/helpers"
	"github.com/migratooor/tokenLists/generators/common/logs"
)

var SUSHI_PAIR_THRESHOLD = 3

func handleSushiswapPairsTokenList(tokensPerChainID map[uint64][]common.Address) []TokenListToken {
	tokensForChainIDSyncMap := initSyncMap(tokensPerChainID)

	// Fetch the basic informations for all the tokens for all the chains
	perChainWG := sync.WaitGroup{}
	perChainWG.Add(len(tokensPerChainID))
	for chainID, list := range tokensPerChainID {
		go func(chainID uint64, list []common.Address) {
			defer perChainWG.Done()
			syncMapRaw, _ := tokensForChainIDSyncMap.Load(chainID)
			syncMap := syncMapRaw.([]TokenListToken)

			tokensInfo := retrieveBasicInformations(chainID, list)
			for _, address := range list {
				if token, ok := tokensInfo[address.Hex()]; ok {
					if token.Name == `` || token.Symbol == `` {
						continue
					}
					if newToken, err := SetToken(
						token.Address,
						token.Name,
						token.Symbol,
						``,
						chainID,
						int(token.Decimals),
					); err == nil {
						syncMap = append(syncMap, newToken)
						tokensForChainIDSyncMap.Store(chainID, syncMap)
					}
				}
			}
		}(chainID, list)
	}
	perChainWG.Wait()

	return extractSyncMap(tokensForChainIDSyncMap)
}

func fetchSushiswapPairsTokenList(extra map[string]interface{}) ([]TokenListToken, map[uint64]string) {
	tokensPerChainID := make(map[uint64][]common.Address)
	allTokens := make(map[string]int)
	lastBlockSync := make(map[uint64]string)

	/**************************************************************************
	** Looping through all the Sushiswap contracts per chainID to read the logs
	** and see the pairs and tokens that are being used.
	** In order to be included, a PAIR must have tokens that are both in at
	** least 10 different pairs.
	**************************************************************************/
	for chainID, sushiContract := range SushiswapContractsPerChainID {
		if !helpers.IsChainIDSupported(chainID) {
			continue
		}
		tokensPerChainID[chainID] = []common.Address{}

		/**********************************************************************
		** Init the RPC and get the current block number to know where to start
		** and end the logs fetching
		**********************************************************************/
		chainIDStr := strconv.FormatUint(chainID, 10)
		lastBlockSyncForChainID := uint64(0)
		if sync, ok := extra[`lastBlockSyncFor_`+chainIDStr]; ok {
			lastBlockSyncForChainID, _ = strconv.ParseUint(sync.(string), 10, 64)
		}
		client := ethereum.GetRPC(chainID)
		currentBlockNumber, _ := client.BlockNumber(context.Background())
		threshold := uint64(100_000)
		if chainID == 56 {
			threshold = uint64(5_000)
		}

		for _, sushiContract := range sushiContract {
			start := sushiContract.BlockNumber.Uint64()
			if (lastBlockSyncForChainID != 0) && (lastBlockSyncForChainID > start) {
				start = lastBlockSyncForChainID
			}
			sushiV2Factory, _ := contracts.NewSushiV2Factory(sushiContract.ContractAddress, client)
			for startBlockToTest := start; startBlockToTest <= currentBlockNumber; startBlockToTest += threshold {
				end := startBlockToTest + threshold
				if end > currentBlockNumber {
					end = currentBlockNumber
				}
				options := &bind.FilterOpts{
					Start:   startBlockToTest,
					End:     &end,
					Context: nil,
				}
				logs.Info(`v2 - start: `, startBlockToTest, ` end: `, end, ` total: `, len(allTokens), ` current block: `, currentBlockNumber, ` chainID: `, chainIDStr)
				if log, err := sushiV2Factory.FilterPairCreated(options, nil, nil); err == nil {
					for log.Next() {
						if log.Error() != nil {
							continue
						}
						if _, ok := allTokens[log.Event.Token0.Hex()]; !ok {
							allTokens[log.Event.Token0.Hex()] = 0
						}
						if _, ok := allTokens[log.Event.Token1.Hex()]; !ok {
							allTokens[log.Event.Token1.Hex()] = 0
						}

						allTokens[log.Event.Token0.Hex()]++
						allTokens[log.Event.Token1.Hex()]++
					}
				} else {
					logs.Error("Error fetching all tokens from sushiswap factory contract: ", err)
					startBlockToTest -= threshold
				}
			}
			lastBlockSync[chainID] = strconv.FormatUint(currentBlockNumber, 10)
		}

		/**********************************************************************
		** Transforming the output to the format that we need for the handle
		** function
		**********************************************************************/
		for address, count := range allTokens {
			if helpers.IsIgnoredToken(chainID, common.HexToAddress(address)) {
				continue
			}
			if count >= SUSHI_PAIR_THRESHOLD {
				tokensPerChainID[chainID] = append(tokensPerChainID[chainID], common.HexToAddress(address))
			}
		}
	}

	return handleSushiswapPairsTokenList(tokensPerChainID), lastBlockSync
}

func buildSushiswapPairsTokenList() {
	tokenList := loadTokenListFromJsonFile(`sushiswap-pairs.json`)
	tokenList.Name = "SushiSwap Token Pairs"
	tokenList.LogoURI = "https://raw.githubusercontent.com/sushiswap/art/master/sushi/logo-256x256.png"

	tokens, lastBlockSync := fetchSushiswapPairsTokenList(tokenList.Metadata)
	if tokenList.Metadata == nil {
		tokenList.Metadata = make(map[string]interface{})
	}
	for chainID, blockNumber := range lastBlockSync {
		chainIDStr := strconv.FormatUint(chainID, 10)
		tokenList.Metadata[`lastBlockSyncFor_`+chainIDStr] = blockNumber
	}

	saveTokenListInJsonFile(tokenList, tokens, `sushiswap-pairs.json`, Append)
}
