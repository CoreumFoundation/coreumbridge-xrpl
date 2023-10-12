#[cfg(test)]
mod tests {
    use coreum_test_tube::{Account, AssetFT, CoreumTestApp, Module, SigningAccount, Wasm};
    use coreum_wasm_sdk::{
        assetft::{BURNING, IBC, MINTING},
        types::coreum::asset::ft::v1::{
            QueryBalanceRequest, QueryParamsRequest, QueryTokensRequest, Token,
        },
    };
    use cosmwasm_std::{coin, coins, Addr, Coin, Uint128};
    use sha2::{Digest, Sha256};

    use crate::{
        error::ContractError,
        evidence::{Evidence, OperationResult},
        msg::{
            AvailableTicketsResponse, CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg,
            InstantiateMsg, PendingOperationsResponse, QueryMsg, XRPLTokensResponse,
        },
        state::{Config, Operation, OperationType, Signature},
    };
    const FEE_DENOM: &str = "ucore";
    const XRP_SYMBOL: &str = "XRP";
    const XRP_SUBUNIT: &str = "drop";
    const COREUM_CURRENCY_PREFIX: &str = "coreum";
    const XRPL_DENOM_PREFIX: &str = "xrpl";

    #[derive(Clone)]
    struct XRPLToken {
        pub issuer: String,
        pub currency: String,
    }

    fn store_and_instantiate(
        wasm: &Wasm<'_, CoreumTestApp>,
        signer: &SigningAccount,
        owner: Addr,
        relayers: Vec<Addr>,
        evidence_threshold: u32,
        used_tickets_threshold: u32,
        issue_fee: Vec<Coin>,
    ) -> String {
        let wasm_byte_code = std::fs::read("./artifacts/coreumbridge_xrpl.wasm").unwrap();
        let code_id = wasm
            .store_code(&wasm_byte_code, None, &signer)
            .unwrap()
            .data
            .code_id;
        wasm.instantiate(
            code_id,
            &InstantiateMsg {
                owner,
                relayers,
                evidence_threshold,
                used_tickets_threshold,
            },
            None,
            "coreumbridge-xrpl".into(),
            &issue_fee,
            &signer,
        )
        .unwrap()
        .data
        .address
    }

    fn query_issue_fee(asset_ft: &AssetFT<'_, CoreumTestApp>) -> Vec<Coin> {
        let issue_fee = asset_ft
            .query_params(&QueryParamsRequest {})
            .unwrap()
            .params
            .unwrap()
            .issue_fee
            .unwrap();
        coins(issue_fee.amount.trim().parse().unwrap(), issue_fee.denom)
    }

    pub fn hash_bytes(bytes: Vec<u8>) -> String {
        let mut hasher = Sha256::new();
        hasher.update(bytes);
        let output = hasher.finalize();
        hex::encode(output)
    }

    #[test]
    fn contract_instantiation() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&[coin(100_000_000_000, FEE_DENOM)])
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        //We check that we can store and instantiate
        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );
        assert!(!contract_addr.is_empty());

        // We check that trying to instantiate with invalid issue fee fails.
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![Addr::unchecked(signer.address())],
                    evidence_threshold: 1,
                    used_tickets_threshold: 50,
                },
                None,
                "label".into(),
                &coins(10, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::InvalidFundsAmount {}.to_string().as_str()));

        // We check that trying to instantiate with invalid max allowed ticket fails.
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![Addr::unchecked(signer.address())],
                    evidence_threshold: 1,
                    used_tickets_threshold: 1,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::InvalidUsedTicketsThreshold {}
                .to_string()
                .as_str()
        ));

        // We query the issued token by the contract instantiation (XRP)
        let query_response = asset_ft
            .query_tokens(&QueryTokensRequest {
                pagination: None,
                issuer: contract_addr.clone(),
            })
            .unwrap();

        assert_eq!(
            query_response.tokens[0],
            Token {
                denom: format!("{}-{}", XRP_SUBUNIT, contract_addr.to_lowercase()),
                issuer: contract_addr.clone(),
                symbol: XRP_SYMBOL.to_string(),
                subunit: XRP_SUBUNIT.to_string(),
                precision: 6,
                description: "".to_string(),
                globally_frozen: false,
                features: vec![
                    MINTING.try_into().unwrap(),
                    BURNING.try_into().unwrap(),
                    IBC.try_into().unwrap()
                ],
                burn_rate: "0".to_string(),
                send_commission_rate: "0".to_string(),
                version: 1
            }
        );
    }

    #[test]
    fn transfer_ownership() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let new_owner = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();
        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        //Query current owner
        let query_owner = wasm
            .query::<QueryMsg, cw_ownable::Ownership<String>>(
                &contract_addr,
                &QueryMsg::Ownership {},
            )
            .unwrap();

        assert_eq!(query_owner.owner.unwrap(), signer.address().to_string());

        // Current owner is going to transfer ownership to another address (new_owner)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateOwnership(cw_ownable::Action::TransferOwnership {
                new_owner: new_owner.address(),
                expiry: None,
            }),
            &vec![],
            &signer,
        )
        .unwrap();

        // New owner is going to accept the ownership
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateOwnership(cw_ownable::Action::AcceptOwnership {}),
            &vec![],
            &new_owner,
        )
        .unwrap();

        let query_owner = wasm
            .query::<QueryMsg, cw_ownable::Ownership<String>>(
                &contract_addr,
                &QueryMsg::Ownership {},
            )
            .unwrap();

        assert_eq!(query_owner.owner.unwrap(), new_owner.address().to_string());

        //Try transfering from old owner again, should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateOwnership(cw_ownable::Action::TransferOwnership {
                    new_owner: new_owner.address(),
                    expiry: None,
                }),
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(transfer_error.to_string().contains(
            ContractError::Ownership(cw_ownable::OwnershipError::NotOwner)
                .to_string()
                .as_str()
        ));
    }

    #[test]
    fn query_config() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();
        assert_eq!(query_config.evidence_threshold, 1);
        assert_eq!(query_config.used_tickets_threshold, 50);
        assert_eq!(
            query_config.relayers,
            vec![Addr::unchecked(signer.address())]
        );
    }

    #[test]
    fn query_xrpl_tokens() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(
            query_xrpl_tokens.tokens[0].coreum_denom,
            format!("{}-{}", XRP_SUBUNIT, &contract_addr.to_lowercase())
        );
    }

    #[test]
    fn register_coreum_token() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        let test_tokens = vec!["test_denom1".to_string(), "test_denom2".to_string()];

        //Register two tokens correctly
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: token,
                    decimals: 6,
                },
                &vec![],
                &signer,
            )
            .unwrap();
        }

        //Register 1 token with same denom, should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: test_tokens[0].clone(),
                    decimals: 6,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(register_error.to_string().contains(
            ContractError::CoreumTokenAlreadyRegistered {
                denom: test_tokens[0].clone()
            }
            .to_string()
            .as_str()
        ));

        //Query 1 token
        let query_coreum_token = wasm
            .query::<QueryMsg, CoreumTokenResponse>(
                &contract_addr,
                &QueryMsg::CoreumToken {
                    denom: test_tokens[0].clone(),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_token.token.xrpl_currency.len(), 16);
        assert!(query_coreum_token
            .token
            .xrpl_currency
            .starts_with(COREUM_CURRENCY_PREFIX));

        //Query all tokens
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 2);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[0]);

        //Query tokens with limit
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    offset: None,
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 1);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[0]);

        //Query tokens with pagination
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    offset: Some(1),
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 1);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[1]);
    }

    #[test]
    fn register_xrpl_token() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        let test_tokens = vec![
            XRPLToken {
                issuer: "issuer1".to_string(),
                currency: "currency1".to_string(),
            },
            XRPLToken {
                issuer: "issuer2".to_string(),
                currency: "currency2".to_string(),
            },
        ];

        //Register token with incorrect fee (too much), should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                },
                &coins(20_000_000, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(register_error
            .to_string()
            .contains(ContractError::InvalidFundsAmount {}.to_string().as_str()));

        //Register two tokens correctly
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer,
                    currency: token.currency,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap();
        }

        // Check tokens are in the bank module
        let asset_ft = AssetFT::new(&app);
        let query_response = asset_ft
            .query_tokens(&QueryTokensRequest {
                pagination: None,
                issuer: contract_addr.clone(),
            })
            .unwrap();

        assert_eq!(query_response.tokens.len(), 3);
        assert!(query_response.tokens[1]
            .denom
            .starts_with(XRPL_DENOM_PREFIX),);
        assert!(query_response.tokens[2]
            .denom
            .starts_with(XRPL_DENOM_PREFIX),);

        //Register 1 token with same issuer+currency, should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(register_error.to_string().contains(
            ContractError::XRPLTokenAlreadyRegistered {
                issuer: test_tokens[0].issuer.clone(),
                currency: test_tokens[0].currency.clone()
            }
            .to_string()
            .as_str()
        ));

        //Query all tokens
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 3);

        //Query all tokens with limit
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: None,
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 1);
        assert_eq!(
            query_xrpl_tokens.tokens[0].coreum_denom,
            format!("{}-{}", XRP_SUBUNIT, &contract_addr.to_lowercase())
        );

        //Query all tokens with pagination
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: Some(1),
                    limit: Some(2),
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 2);
        assert!(query_xrpl_tokens.tokens[0]
            .coreum_denom
            .starts_with(XRPL_DENOM_PREFIX));
        assert_eq!(
            query_xrpl_tokens.tokens[0].issuer.clone(),
            test_tokens[0].issuer
        );
        assert_eq!(
            query_xrpl_tokens.tokens[0].currency.clone(),
            test_tokens[0].currency
        );
        assert!(query_xrpl_tokens.tokens[1]
            .coreum_denom
            .starts_with(XRPL_DENOM_PREFIX));
        assert_eq!(
            query_xrpl_tokens.tokens[1].issuer.clone(),
            test_tokens[1].issuer
        );
        assert_eq!(
            query_xrpl_tokens.tokens[1].currency.clone(),
            test_tokens[1].currency
        );
    }

    #[test]
    fn send_from_xrpl_to_coreum() {
        let app = CoreumTestApp::new();
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), 4)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let relayer1 = accounts.get(1).unwrap();
        let relayer2 = accounts.get(2).unwrap();
        let receiver = accounts.get(3).unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        //Test with 1 relayer and 1 evidence threshold first
        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(relayer1.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        let test_token = XRPLToken {
            issuer: "issuer1".to_string(),
            currency: "currency1".to_string(),
        };

        //Register a token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token.issuer.clone(),
                currency: test_token.currency.clone(),
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        let denom = query_xrpl_tokens.tokens[1].coreum_denom.clone();
        let hash = "random_hash".to_string();
        let amount = Uint128::from(100 as u128);

        //Bridge with 1 relayer should immediately mint and send to the receiver address
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer1,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount.to_string());

        //Test with more than 1 relayer
        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![
                Addr::unchecked(relayer1.address()),
                Addr::unchecked(relayer2.address()),
            ],
            2,
            50,
            query_issue_fee(&asset_ft),
        );

        //Register a token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token.issuer.clone(),
                currency: test_token.currency.clone(),
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        let denom = query_xrpl_tokens.tokens[1].coreum_denom.clone();

        //Trying to send from an address that is not a relayer should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                signer,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        //Trying to send a token that is not previously registered should also fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: "not_registered".to_string(),
                        currency: "not_registered".to_string(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer1,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        //Trying to send invalid evidence should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: Uint128::from(0 as u128),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer1,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::InvalidAmount {}.to_string().as_str()));

        //First relayer to execute should not trigger a mint and send
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer1,
        )
        .unwrap();

        //Balance should be 0
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "0".to_string());

        //Relaying again from same relayer should trigger an error
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer1,
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::EvidenceAlreadyProvided {}
                .to_string()
                .as_str()
        ));

        //Second relayer to execute should trigger a mint and send
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer2,
        )
        .unwrap();

        //Balance should be 0
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount.to_string());

        //Trying to relay again will trigger an error because operation is already executed
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer2,
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));

        let new_amount = Uint128::from(150 as u128);
        //Trying to relay a different operation with same hash will trigger an error
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: new_amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer1,
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));
    }

    #[test]
    fn ticket_recovery() {
        let app = CoreumTestApp::new();
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), 4)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let relayer1 = accounts.get(1).unwrap();
        let relayer2 = accounts.get(2).unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![
                Addr::unchecked(relayer1.address()),
                Addr::unchecked(relayer2.address()),
            ],
            2,
            50,
            query_issue_fee(&asset_ft),
        );

        // Querying current pending operations and available tickets should return empty results.
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations, vec![]);
        assert_eq!(query_available_tickets.tickets, Vec::<u64>::new());

        let sequence_number = 1;
        //Trying to recover 0 tickets will fail
        let recover_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    sequence_number,
                    number_of_tickets: Some(0),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_ticket_error.to_string().contains(
            ContractError::InvalidTicketNumberToAllocate {}
                .to_string()
                .as_str()
        ));

        // Owner will send a recover tickets operation which will set the pending ticket update flag to true
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                sequence_number,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Try to send another one will fail because there is a pending update operation that hasn't been processed
        let recover_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    sequence_number,
                    number_of_tickets: Some(5),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_ticket_error
            .to_string()
            .contains(ContractError::PendingTicketUpdate {}.to_string().as_str()));

        // Querying the current pending operations should return 1
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(
            query_pending_operations.operations,
            [Operation {
                ticket_number: None,
                sequence_number: Some(sequence_number),
                signatures: vec![], //No signatures yet
                operation_type: OperationType::AllocateTickets { number: 5 }
            }]
        );

        // Querying with pagination values should return the same
        let query_pending_operations_with_pagination = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: Some(0),
                    limit: Some(1),
                },
            )
            .unwrap();

        assert_eq!(
            query_pending_operations_with_pagination.operations,
            [Operation {
                ticket_number: None,
                sequence_number: Some(sequence_number),
                signatures: vec![],
                operation_type: OperationType::AllocateTickets { number: 5 }
            }]
        );

        let tx_hash = "random_hash".to_string();
        let sequence_number = 1;
        let tickets = vec![1, 2, 3, 4, 5];
        let correct_signature_example = "3045022100DFA01DA5D6C9877F9DAA59A06032247F3D7ED6444EAD5C90A3AC33CCB7F19B3F02204D8D50E4D085BB1BC9DFB8281B8F35BDAEB7C74AE4B825F8CAE1217CFBDF4EA1".to_string();

        // Trying to relay the operation with a different sequence number than the one in pending operation should fail.
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: tx_hash.clone(),
                        sequence_number: Some(sequence_number + 1),
                        ticket_number: None,
                        confirmed: false,
                        operation_result: OperationResult::TicketsAllocation {
                            tickets: Some(tickets.clone()),
                        },
                    },
                },
                &vec![],
                &relayer1,
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::PendingOperationNotFound {}
                .to_string()
                .as_str()
        ));

        //Providing a signature with the wrong length should fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterSignature {
                    operation_id: sequence_number,
                    signature: "wrong_signature_example".to_string(),
                },
                &vec![],
                &relayer1,
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::InvalidSignatureLength {}
                .to_string()
                .as_str()
        ));

        //Provide signatures for the operation for each relayer
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterSignature {
                operation_id: sequence_number,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            &relayer1,
        )
        .unwrap();

        //Provide the signature again for the operation will fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterSignature {
                    operation_id: sequence_number,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                &relayer1,
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::SignatureAlreadyProvided {}
                .to_string()
                .as_str()
        ));

        //Provide a signature for an operation that is not pending should fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterSignature {
                    operation_id: sequence_number + 1,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                &relayer1,
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::PendingOperationNotFound {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterSignature {
                operation_id: sequence_number,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            &relayer2,
        )
        .unwrap();

        //Verify that we have both signatures in the operation
        let query_pending_operation = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operation.operations.len(), 1);
        assert_eq!(
            query_pending_operation.operations[0].signatures,
            vec![
                Signature {
                    signature: correct_signature_example.clone(),
                    relayer: Addr::unchecked(relayer1.address()),
                },
                Signature {
                    signature: correct_signature_example.clone(),
                    relayer: Addr::unchecked(relayer2.address()),
                }
            ]
        );

        //Relaying the rejected operation twice should remove it from pending operations but not allocate tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: tx_hash.clone(),
                    sequence_number: Some(sequence_number),
                    ticket_number: None,
                    confirmed: false,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            &relayer1,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: tx_hash.clone(),
                    sequence_number: Some(sequence_number),
                    ticket_number: None,
                    confirmed: false,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            &relayer2,
        )
        .unwrap();

        // Querying current pending operations and tickets should return empty results again
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations, vec![]);
        assert_eq!(query_available_tickets.tickets, Vec::<u64>::new());

        // Let's do the same now but confirming the operation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                sequence_number,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        //We provide the signatures again
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterSignature {
                operation_id: sequence_number,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            &relayer1,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterSignature {
                operation_id: sequence_number,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            &relayer2,
        )
        .unwrap();
        // Trying to relay the operation with a same hash as previous rejected one should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: tx_hash.clone(),
                        sequence_number: Some(sequence_number),
                        ticket_number: None,
                        confirmed: true,
                        operation_result: OperationResult::TicketsAllocation {
                            tickets: Some(tickets.clone()),
                        },
                    },
                },
                &vec![],
                &relayer1,
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));

        let tx_hash = "random_hash2".to_string();

        //Relaying the confirmed operation twice should remove it from pending operations and allocate tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: tx_hash.clone(),
                    sequence_number: Some(sequence_number),
                    ticket_number: None,
                    confirmed: true,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            &relayer1,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: tx_hash.clone(),
                    sequence_number: Some(sequence_number),
                    ticket_number: None,
                    confirmed: true,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            &relayer2,
        )
        .unwrap();

        // Querying the current pending operations should return empty
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();

        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations, vec![]);
        assert_eq!(query_available_tickets.tickets, tickets.clone());
    }

    #[test]
    fn unauthorized_access() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let not_owner = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
            query_issue_fee(&asset_ft),
        );

        //Try transfering from user that is not owner, should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateOwnership(cw_ownable::Action::TransferOwnership {
                    new_owner: not_owner.address(),
                    expiry: None,
                }),
                &vec![],
                &not_owner,
            )
            .unwrap_err();

        assert!(transfer_error.to_string().contains(
            ContractError::Ownership(cw_ownable::OwnershipError::NotOwner)
                .to_string()
                .as_str()
        ));

        //Try registering a coreum token as not_owner, should fail
        let register_coreum_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "random_denom".to_string(),
                    decimals: 6,
                },
                &vec![],
                &not_owner,
            )
            .unwrap_err();

        assert!(register_coreum_error.to_string().contains(
            ContractError::Ownership(cw_ownable::OwnershipError::NotOwner)
                .to_string()
                .as_str()
        ));

        //Try registering an XRPL token as not_owner, should fail
        let register_xrpl_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: "issuer".to_string(),
                    currency: "currency".to_string(),
                },
                &query_issue_fee(&asset_ft),
                &not_owner,
            )
            .unwrap_err();

        assert!(register_xrpl_error.to_string().contains(
            ContractError::Ownership(cw_ownable::OwnershipError::NotOwner)
                .to_string()
                .as_str()
        ));

        //Trying to send from an address that is not a relayer should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: "random_hash".to_string(),
                        issuer: "random_issuer".to_string(),
                        currency: "random_currency".to_string(),
                        amount: Uint128::from(100 as u128),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                &not_owner,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        //Try recovering tickets as not_owner, should fail
        let recover_tickets = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    sequence_number: 1,
                    number_of_tickets: Some(5),
                },
                &[],
                &not_owner,
            )
            .unwrap_err();

        assert!(recover_tickets.to_string().contains(
            ContractError::Ownership(cw_ownable::OwnershipError::NotOwner)
                .to_string()
                .as_str()
        ));
    }

    #[test]
    fn enum_hashes() {
        let evidence1 = Evidence::XRPLToCoreumTransfer {
            tx_hash: "random_hash".to_string(),
            issuer: "random_issuer".to_string(),
            currency: "random_currency".to_string(),
            amount: Uint128::from(100 as u128),
            recipient: Addr::unchecked("signer"),
        };

        let evidence2 = Evidence::XRPLToCoreumTransfer {
            tx_hash: "random_hash".to_string(),
            issuer: "random_issuer".to_string(),
            currency: "random_currency".to_string(),
            amount: Uint128::from(101 as u128),
            recipient: Addr::unchecked("signer"),
        };

        assert_eq!(
            hash_bytes(serde_json::to_string(&evidence1).unwrap().into_bytes()),
            hash_bytes(
                serde_json::to_string(&evidence1.clone())
                    .unwrap()
                    .into_bytes()
            )
        );

        assert_ne!(
            hash_bytes(serde_json::to_string(&evidence1).unwrap().into_bytes()),
            hash_bytes(serde_json::to_string(&evidence2).unwrap().into_bytes())
        );

        let evidence3 = Evidence::XRPLTransactionResult {
            tx_hash: "random_hash123".to_string(),
            sequence_number: Some(1),
            ticket_number: None,
            confirmed: false,
            operation_result: OperationResult::TicketsAllocation {
                tickets: Some(vec![1, 2, 3, 4, 5]),
            },
        };

        let evidence4 = Evidence::XRPLTransactionResult {
            tx_hash: "random_hash123".to_string(),
            sequence_number: Some(1),
            ticket_number: None,
            confirmed: true,
            operation_result: OperationResult::TicketsAllocation {
                tickets: Some(vec![1, 2, 3, 4, 5]),
            },
        };

        assert_ne!(
            hash_bytes(serde_json::to_string(&evidence3).unwrap().into_bytes()),
            hash_bytes(serde_json::to_string(&evidence4).unwrap().into_bytes()),
        );
    }
}
