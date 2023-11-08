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
    use rand::{distributions::Alphanumeric, thread_rng, Rng};
    use sha2::{Digest, Sha256};

    use crate::{
        error::ContractError,
        evidence::{Evidence, OperationResult, TransactionResult},
        msg::{
            AvailableTicketsResponse, CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg,
            InstantiateMsg, PendingOperationsResponse, QueryMsg, XRPLTokensResponse,
        },
        operation::{Operation, OperationType},
        relayer::Relayer,
        signatures::Signature,
        state::{Config, TokenState, XRPLToken as QueriedXRPLToken},
    };

    const FEE_DENOM: &str = "ucore";
    const XRP_SYMBOL: &str = "XRP";
    const XRP_SUBUNIT: &str = "drop";
    const XRPL_DENOM_PREFIX: &str = "xrpl";
    const XRP_CURRENCY: &str = "XRP";
    const XRP_ISSUER: &str = "rrrrrrrrrrrrrrrrrrrrrho";
    const TRUST_SET_LIMIT_AMOUNT: u128 = 1000000000000000000; // 1e18
    const XRP_DECIMALS: u32 = 6;
    const XRP_DEFAULT_SENDING_PRECISION: i32 = 6;
    const XRP_DEFAULT_MAX_HOLDING_AMOUNT: u128 =
        10u128.pow(16 - XRP_DEFAULT_SENDING_PRECISION as u32 + XRP_DECIMALS);

    #[derive(Clone)]
    struct XRPLToken {
        pub issuer: String,
        pub currency: String,
        pub sending_precision: i32,
        pub max_holding_amount: u128,
    }

    fn store_and_instantiate(
        wasm: &Wasm<'_, CoreumTestApp>,
        signer: &SigningAccount,
        owner: Addr,
        relayers: Vec<Relayer>,
        evidence_threshold: u32,
        used_ticket_sequence_threshold: u32,
        trust_set_limit_amount: Uint128,
        issue_fee: Vec<Coin>,
    ) -> String {
        let wasm_byte_code = std::fs::read("../build/coreumbridge_xrpl.wasm").unwrap();
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
                used_ticket_sequence_threshold,
                trust_set_limit_amount,
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
        coins(issue_fee.amount.parse().unwrap(), issue_fee.denom)
    }

    pub fn hash_bytes(bytes: Vec<u8>) -> String {
        let mut hasher = Sha256::new();
        hasher.update(bytes);
        let output = hasher.finalize();
        hex::encode(output)
    }

    pub fn generate_hash() -> String {
        String::from_utf8(
            thread_rng()
                .sample_iter(&Alphanumeric)
                .take(20)
                .collect::<Vec<_>>(),
        )
        .unwrap()
    }

    pub fn generate_xrpl_address() -> String {
        let mut address = 'r'.to_string();
        let rand = String::from_utf8(
            thread_rng()
                .sample_iter(&Alphanumeric)
                .take(30)
                .collect::<Vec<_>>(),
        )
        .unwrap();

        address.push_str(rand.as_str());
        address
    }

    pub fn generate_xrpl_pub_key() -> String {
        String::from_utf8(
            thread_rng()
                .sample_iter(&Alphanumeric)
                .take(52)
                .collect::<Vec<_>>(),
        )
        .unwrap()
    }

    #[test]
    fn contract_instantiation() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&[coin(100_000_000_000, FEE_DENOM)])
            .unwrap();
        let relayer_account = app
            .init_account(&[coin(100_000_000_000, FEE_DENOM)])
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let xrpl_address = generate_xrpl_address();
        let xrpl_pub_key = generate_xrpl_pub_key();

        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: xrpl_address.clone(),
            xrpl_pub_key: xrpl_pub_key.clone(),
        };

        let relayer_duplicated_xrpl_address = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: xrpl_address,
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let relayer_duplicated_xrpl_pub_key = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key,
        };

        let relayer_duplicated_coreum_address = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let relayer_correct = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        // We check that we can store and instantiate
        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone(), relayer_correct.clone()],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );
        assert!(!contract_addr.is_empty());

        // We check that trying to instantiate with relayers with the same xrpl address fails
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer.clone(), relayer_duplicated_xrpl_address.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::DuplicatedRelayerXRPLAddress {}
                .to_string()
                .as_str()
        ));

        // We check that trying to instantiate with relayers with the same xrpl public key fails
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer.clone(), relayer_duplicated_xrpl_pub_key.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::DuplicatedRelayerXRPLPubKey {}
                .to_string()
                .as_str()
        ));

        // We check that trying to instantiate with relayers with the same coreum address fails
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer.clone(), relayer_duplicated_coreum_address.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::DuplicatedRelayerCoreumAddress {}
                .to_string()
                .as_str()
        ));

        // We check that trying to instantiate with invalid issue fee fails.
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
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
                    relayers: vec![relayer],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 1,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::InvalidUsedTicketSequenceThreshold {}
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        // Query current owner
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

        // Try transfering from old owner again, should fail
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(
            query_config,
            Config {
                relayers: vec![relayer],
                evidence_threshold: 1,
                used_ticket_sequence_threshold: 50,
                trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT)
            }
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
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
            query_xrpl_tokens.tokens[0],
            QueriedXRPLToken {
                issuer: XRP_ISSUER.to_string(),
                currency: XRP_CURRENCY.to_string(),
                coreum_denom: format!("{}-{}", XRP_SUBUNIT, contract_addr).to_lowercase(),
                sending_precision: XRP_DEFAULT_SENDING_PRECISION,
                max_holding_amount: Uint128::new(XRP_DEFAULT_MAX_HOLDING_AMOUNT),
                state: TokenState::Enabled,
            }
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let test_tokens = vec!["test_denom1".to_string(), "test_denom2".to_string()];

        // Register two tokens correctly
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

        // Register 1 token with same denom, should fail
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

        // Query 1 token
        let query_coreum_token = wasm
            .query::<QueryMsg, CoreumTokenResponse>(
                &contract_addr,
                &QueryMsg::CoreumToken {
                    denom: test_tokens[0].clone(),
                },
            )
            .unwrap();

        assert_eq!(query_coreum_token.token.xrpl_currency.len(), 40);
        assert!(query_coreum_token.token.xrpl_currency.ends_with("00000000"));
        assert!(query_coreum_token
            .token
            .xrpl_currency
            .chars()
            .all(|c| c.is_ascii_hexdigit()));

        // Query all tokens
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

        // Query tokens with limit
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

        // Query tokens with pagination
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            2,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let test_tokens = vec![
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "USD".to_string(),     // Valid standard currency code
                sending_precision: -15,
                max_holding_amount: 100,
            },
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "015841551A748AD2C1F76FF6ECB0CCCD00000000".to_string(), // Valid hexadecimal currency
                sending_precision: 15,
                max_holding_amount: 50000,
            },
        ];

        // Registering a token with an invalid issuer should fail.
        let issuer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: "not_valid_issuer".to_string(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: test_tokens[0].sending_precision.clone(),
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount.clone()),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(issuer_error
            .to_string()
            .contains(ContractError::InvalidXRPLIssuer {}.to_string().as_str()));

        // Registering a token with an invalid precision should fail.
        let issuer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: -16,
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount.clone()),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(issuer_error.to_string().contains(
            ContractError::InvalidSendingPrecision {}
                .to_string()
                .as_str()
        ));

        // Registering a token with an invalid precision should fail.
        let issuer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: 16,
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount.clone()),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(issuer_error.to_string().contains(
            ContractError::InvalidSendingPrecision {}
                .to_string()
                .as_str()
        ));

        // Registering a token with a valid issuer but invalid currency should fail.
        let currency_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: "invalid_currency".to_string(),
                    sending_precision: test_tokens[1].sending_precision.clone(),
                    max_holding_amount: Uint128::new(test_tokens[1].max_holding_amount.clone()),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(currency_error
            .to_string()
            .contains(ContractError::InvalidXRPLCurrency {}.to_string().as_str()));

        // Register token with incorrect fee (too much), should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: test_tokens[0].sending_precision.clone(),
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount.clone()),
                },
                &coins(20_000_000, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(register_error
            .to_string()
            .contains(ContractError::InvalidFundsAmount {}.to_string().as_str()));

        // Registering a token without having tickets for the TrustSet operation should fail
        let available_tickets_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: test_tokens[0].sending_precision,
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(available_tickets_error
            .to_string()
            .contains(ContractError::NoAvailableTickets {}.to_string().as_str()));

        // Register two tokens correctly
        // Set up enough tickets first to allow registering tokens.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(3),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(vec![1, 2, 3]),
                    },
                },
            },
            &vec![],
            &signer,
        )
        .unwrap();

        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer,
                    currency: token.currency,
                    sending_precision: token.sending_precision,
                    max_holding_amount: Uint128::new(token.max_holding_amount),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap();
        }

        // Trying to register another token would fail because there is only 1 ticket left and that one is reserved
        let extra_token = XRPLToken {
            issuer: generate_xrpl_address(), // Valid issuer
            currency: "USD".to_string(),     // Valid standard currency code
            sending_precision: -15,
            max_holding_amount: 100,
        };

        let last_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: extra_token.issuer,
                    currency: extra_token.currency,
                    sending_precision: extra_token.sending_precision,
                    max_holding_amount: Uint128::new(extra_token.max_holding_amount),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(last_ticket_error
            .to_string()
            .contains(ContractError::LastTicketReserved {}.to_string().as_str()));

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

        // Register 1 token with same issuer+currency, should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: test_tokens[0].sending_precision.clone(),
                    max_holding_amount: Uint128::new(test_tokens[0].max_holding_amount.clone()),
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

        // Query all tokens
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

        // Query all tokens with limit
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
        assert!(query_xrpl_tokens.tokens[0].coreum_denom.starts_with("xrpl"));

        // Query all tokens with pagination
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
    }

    #[test]
    fn send_from_xrpl_to_coreum() {
        let app = CoreumTestApp::new();
        let accounts_number = 4;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let receiver = accounts.get((accounts_number - 2) as usize).unwrap();
        let xrpl_addresses = vec![generate_xrpl_address(), generate_xrpl_address()];

        let xrpl_pub_keys = vec![generate_xrpl_pub_key(), generate_xrpl_pub_key()];

        let mut relayer_accounts = vec![];
        let mut relayers = vec![];

        for i in 0..accounts_number - 2 {
            relayer_accounts.push(accounts.get(i as usize).unwrap());
            relayers.push(Relayer {
                coreum_address: Addr::unchecked(accounts.get(i as usize).unwrap().address()),
                xrpl_address: xrpl_addresses[i as usize].to_string(),
                xrpl_pub_key: xrpl_pub_keys[i as usize].to_string(),
            });
        }

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        // Test with 1 relayer and 1 evidence threshold first
        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayers[0].clone()],
            1,
            2,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let test_token = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "USD".to_string(),
            sending_precision: 15,
            max_holding_amount: 50000,
        };

        // Set up enough tickets first to allow registering tokens.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(3),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(vec![1, 2, 3]),
                    },
                },
            },
            &vec![],
            &relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token.issuer.clone(),
                currency: test_token.currency.clone(),
                sending_precision: test_token.sending_precision.clone(),
                max_holding_amount: Uint128::new(test_token.max_holding_amount.clone()),
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

        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token.issuer && t.currency == test_token.currency)
            .unwrap()
            .coreum_denom
            .clone();

        let hash = generate_hash();
        let amount = Uint128::new(100);

        // Bridging with 1 relayer before activating the token should return an error
        let not_active_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(not_active_error
            .to_string()
            .contains(ContractError::XRPLTokenNotEnabled {}.to_string().as_str()));

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                    },
                },
            },
            &[],
            &relayer_accounts[0],
        )
        .unwrap();

        // Bridge with 1 relayer should immediately mint and send to the receiver address
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount.to_string());

        // Test with more than 1 relayer
        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayers[0].clone(), relayers[1].clone()],
            2,
            2,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        // Set up enough tickets first to allow registering tokens.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(3),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let hash2 = generate_hash();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(hash2.clone()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(vec![1, 2, 3]),
                    },
                },
            },
            &vec![],
            &relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(hash2),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(vec![1, 2, 3]),
                    },
                },
            },
            &vec![],
            &relayer_accounts[1],
        )
        .unwrap();

        // Register a token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token.issuer.clone(),
                currency: test_token.currency.clone(),
                sending_precision: test_token.sending_precision,
                max_holding_amount: Uint128::new(test_token.max_holding_amount),
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        let tx_hash = generate_hash();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                    },
                },
            },
            &[],
            &relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                    },
                },
            },
            &[],
            &relayer_accounts[1],
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

        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token.issuer && t.currency == test_token.currency)
            .unwrap()
            .coreum_denom
            .clone();

        // Trying to send from an address that is not a relayer should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
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

        // Trying to send a token that is not previously registered should also fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: "not_registered".to_string(),
                        currency: "not_registered".to_string(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // Trying to send invalid evidence should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: Uint128::new(0),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::InvalidAmount {}.to_string().as_str()));

        // First relayer to execute should not trigger a mint and send
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Balance should be 0
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "0".to_string());

        // Relaying again from same relayer should trigger an error
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::EvidenceAlreadyProvided {}
                .to_string()
                .as_str()
        ));

        // Second relayer to execute should trigger a mint and send
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: hash.clone(),
                    issuer: test_token.issuer.clone(),
                    currency: test_token.currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        // Balance should be 0
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount.to_string());

        // Trying to relay again will trigger an error because operation is already executed
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));

        let new_amount = Uint128::new(150);
        // Trying to relay a different operation with same hash will trigger an error
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: hash.clone(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: new_amount.clone(),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));
    }

    #[test]
    fn precisions() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let receiver = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            7,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let test_token1 = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "TT1".to_string(),
            sending_precision: -2,
            max_holding_amount: 200000000000000000,
        };
        let test_token2 = XRPLToken {
            issuer: generate_xrpl_address().to_string(),
            currency: "TT2".to_string(),
            sending_precision: 13,
            max_holding_amount: 499,
        };

        let test_token3 = XRPLToken {
            issuer: generate_xrpl_address().to_string(),
            currency: "TT3".to_string(),
            sending_precision: 0,
            max_holding_amount: 5000000000000000,
        };

        // Set up enough tickets first to allow registering tokens.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(8),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(vec![1, 2, 3, 4, 5, 6, 7, 8]),
                    },
                },
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Test negative sending precisions

        // Register token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token1.issuer.clone(),
                currency: test_token1.currency.clone(),
                sending_precision: test_token1.sending_precision.clone(),
                max_holding_amount: Uint128::new(test_token1.max_holding_amount.clone()),
            },
            &query_issue_fee(&asset_ft),
            &signer,
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

        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token1.issuer && t.currency == test_token1.currency)
            .unwrap()
            .coreum_denom
            .clone();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token1.issuer.clone(),
                        currency: test_token1.currency.clone(),
                    },
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token1.issuer.clone(),
                        currency: test_token1.currency.clone(),
                        // Sending less than 100000000000000000, in this case 99999999999999999 (1 less digit) should return an error because it will truncate to zero
                        amount: Uint128::new(99999999999999999),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token1.issuer.clone(),
                    currency: test_token1.currency.clone(),
                    // Sending more than 199999999999999999 will truncate to 100000000000000000 and send it to the user
                    amount: Uint128::new(199999999999999999),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "100000000000000000".to_string());

        // Sending it again should work too because we will not have passed maximum holding amount
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token1.issuer.clone(),
                    currency: test_token1.currency.clone(),
                    // Let's try sending 199999999999999999 that will be truncated to 100000000000000000 and send it to the user
                    amount: Uint128::new(199999999999999999),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "200000000000000000".to_string());

        // Sending it a 3rd time will fail because will pass the maximum holding amount.
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token1.issuer.clone(),
                        currency: test_token1.currency.clone(),
                        // Let's try sending 199999999999999999 that will be truncated to 100000000000000000 and send it to the user
                        amount: Uint128::new(199999999999999999),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(maximum_amount_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "200000000000000000".to_string());

        // Test positive sending precisions

        // Register token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token2.issuer.clone(),
                currency: test_token2.currency.clone(),
                sending_precision: test_token2.sending_precision.clone(),
                max_holding_amount: Uint128::new(test_token2.max_holding_amount.clone()),
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
                    },
                },
            },
            &[],
            &signer,
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

        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token2.issuer && t.currency == test_token2.currency)
            .unwrap()
            .coreum_denom
            .clone();

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
                        // Sending more than 499 should fail because maximum holding amount is 499
                        amount: Uint128::new(500),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
                        // Sending less than 100 will truncate to 0 so should fail
                        amount: Uint128::new(99),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token2.issuer.clone(),
                    currency: test_token2.currency.clone(),
                    // Sending 299 should truncate the amount to 200
                    amount: Uint128::new(299),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "200".to_string());

        // Sending it again should truncate the amount to 200 again and should pass
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token2.issuer.clone(),
                    currency: test_token2.currency.clone(),
                    // Sending 299 should truncate the amount to 200
                    amount: Uint128::new(299),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "400".to_string());

        // Sending 199 should truncate to 100 and since maximum is 499, it should fail
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
                        // Sending 199 should truncate to 100 and since it's over the maximum it should fail
                        amount: Uint128::new(199),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(maximum_amount_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        // Test 0 sending precision

        // Register token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token3.issuer.clone(),
                currency: test_token3.currency.clone(),
                sending_precision: test_token3.sending_precision.clone(),
                max_holding_amount: Uint128::new(test_token3.max_holding_amount.clone()),
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TrustSet {
                        issuer: test_token3.issuer.clone(),
                        currency: test_token3.currency.clone(),
                    },
                },
            },
            &[],
            &signer,
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

        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token3.issuer && t.currency == test_token3.currency)
            .unwrap()
            .coreum_denom
            .clone();

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token3.issuer.clone(),
                        currency: test_token3.currency.clone(),
                        // Sending more than 5000000000000000 should fail because maximum holding amount is 5000000000000000
                        amount: Uint128::new(6000000000000000),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token3.issuer.clone(),
                        currency: test_token3.currency.clone(),
                        // Sending less than 1000000000000000 will truncate to 0 so should fail
                        amount: Uint128::new(900000000000000),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token3.issuer.clone(),
                    currency: test_token3.currency.clone(),
                    // Sending 1111111111111111 should truncate the amount to 1000000000000000
                    amount: Uint128::new(1111111111111111),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "1000000000000000".to_string());

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token3.issuer.clone(),
                    currency: test_token3.currency.clone(),
                    // Sending 4111111111111111 should truncate the amount to 4000000000000000 and should pass because maximum is 5000000000000000
                    amount: Uint128::new(4111111111111111),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "5000000000000000".to_string());

        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
                        // Sending 1111111111111111 should truncate the amount to 1000000000000000 and should fail because bridge is already holding maximum
                        amount: Uint128::new(1111111111111111),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(maximum_amount_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        // Test sending XRP
        let denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == XRP_ISSUER.to_string() && t.currency == XRP_CURRENCY.to_string())
            .unwrap()
            .coreum_denom
            .clone();

        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: XRP_ISSUER.to_string(),
                        currency: XRP_CURRENCY.to_string(),
                        // Sending more than 100000000000000000 should fail because maximum holding amount is 10000000000000000 (1 less zero)
                        amount: Uint128::new(100000000000000000),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    // There should never be truncation because we allow full precision for XRP initially
                    amount: Uint128::new(1),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "1".to_string());

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    // This should work because we are sending the rest to reach the maximum amount
                    amount: Uint128::new(9999999999999999),
                    recipient: Addr::unchecked(receiver.address()),
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "10000000000000000".to_string());

        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: XRP_ISSUER.to_string(),
                        currency: XRP_CURRENCY.to_string(),
                        // Sending 1 more token would surpass the maximum so should fail
                        amount: Uint128::new(1),
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                &signer,
            )
            .unwrap_err();

        assert!(maximum_amount_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));
    }

    #[test]
    fn ticket_recovery() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let xrpl_addresses = vec![generate_xrpl_address(), generate_xrpl_address()];

        let xrpl_pub_keys = vec![generate_xrpl_pub_key(), generate_xrpl_pub_key()];

        let mut relayer_accounts = vec![];
        let mut relayers = vec![];

        for i in 0..accounts_number - 1 {
            relayer_accounts.push(accounts.get(i as usize).unwrap());
            relayers.push(Relayer {
                coreum_address: Addr::unchecked(accounts.get(i as usize).unwrap().address()),
                xrpl_address: xrpl_addresses[i as usize].to_string(),
                xrpl_pub_key: xrpl_pub_keys[i as usize].to_string(),
            });
        }

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayers[0].clone(), relayers[1].clone()],
            2,
            4,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        // Querying current pending operations and available tickets should return empty results.
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
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

        let account_sequence = 1;
        // Trying to recover tickets with the value less than used_ticket_sequence_threshold
        let recover_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    account_sequence,
                    number_of_tickets: Some(1),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_ticket_error.to_string().contains(
            ContractError::InvalidTicketSequenceToAllocate {}
                .to_string()
                .as_str()
        ));

        // Trying to recover more than max tickets will fail
        let recover_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    account_sequence,
                    number_of_tickets: Some(300),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_ticket_error.to_string().contains(
            ContractError::InvalidTicketSequenceToAllocate {}
                .to_string()
                .as_str()
        ));

        // Trying to recover more than max tickets will fail
        let recover_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    account_sequence,
                    number_of_tickets: Some(300),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_ticket_error.to_string().contains(
            ContractError::InvalidTicketSequenceToAllocate {}
                .to_string()
                .as_str()
        ));

        // Owner will send a recover tickets operation which will set the pending ticket update flag to true
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence,
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
                    account_sequence,
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
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        assert_eq!(
            query_pending_operations.operations,
            [Operation {
                ticket_sequence: None,
                account_sequence: Some(account_sequence),
                signatures: vec![], // No signatures yet
                operation_type: OperationType::AllocateTickets { number: 5 }
            }]
        );

        let tx_hash = generate_hash();
        let tickets = vec![1, 2, 3, 4, 5];
        let correct_signature_example = "3045022100DFA01DA5D6C9877F9DAA59A06032247F3D7ED6444EAD5C90A3AC33CCB7F19B3F02204D8D50E4D085BB1BC9DFB8281B8F35BDAEB7C74AE4B825F8CAE1217CFBDF4EA1".to_string();

        // Trying to relay the operation with a different sequence number than the one in pending operation should fail.
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(account_sequence + 1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Rejected,
                        operation_result: OperationResult::TicketsAllocation { tickets: None },
                    },
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::PendingOperationNotFound {}
                .to_string()
                .as_str()
        ));

        // Provide signatures for the operation for each relayer
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap();

        // Provide the signature again for the operation will fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveSignature {
                    operation_id: account_sequence,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::SignatureAlreadyProvided {}
                .to_string()
                .as_str()
        ));

        // Provide a signature for an operation that is not pending should fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveSignature {
                    operation_id: account_sequence + 1,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::PendingOperationNotFound {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();

        // Verify that we have both signatures in the operation
        let query_pending_operation = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
            )
            .unwrap();

        assert_eq!(query_pending_operation.operations.len(), 1);
        assert_eq!(
            query_pending_operation.operations[0].signatures,
            vec![
                Signature {
                    signature: correct_signature_example.clone(),
                    relayer_coreum_address: Addr::unchecked(relayers[0].coreum_address.clone()),
                },
                Signature {
                    signature: correct_signature_example.clone(),
                    relayer_coreum_address: Addr::unchecked(relayers[1].coreum_address.clone()),
                }
            ]
        );

        // Relaying the rejected operation twice should remove it from pending operations but not allocate tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Rejected,
                    operation_result: OperationResult::TicketsAllocation { tickets: None },
                },
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Rejected,
                    operation_result: OperationResult::TicketsAllocation { tickets: None },
                },
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();

        // Querying current pending operations and tickets should return empty results again
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
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

        // Let's do the same now but reporting an invalid transaction
        let account_sequence = 2;
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // We provide the signatures again
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                signature: correct_signature_example.clone(),
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();
        // Trying to relay the operation with a same hash as previous rejected one should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(account_sequence),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: OperationResult::TicketsAllocation {
                            tickets: Some(tickets.clone()),
                        },
                    },
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(relayer_error.to_string().contains(
            ContractError::OperationAlreadyExecuted {}
                .to_string()
                .as_str()
        ));

        // Relaying the operation twice as invalid should removed it from pending operations and not allocate tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: None,
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Invalid,
                    operation_result: OperationResult::TicketsAllocation { tickets: None },
                },
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: None,
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Invalid,
                    operation_result: OperationResult::TicketsAllocation { tickets: None },
                },
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();

        // Querying the current pending operations should return empty
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
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
                account_sequence,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();

        // Relaying the accepted operation twice should remove it from pending operations and allocate tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(account_sequence),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    },
                },
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();

        // Querying the current pending operations should return empty
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {},
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
    fn invalid_transaction_evidences() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            4,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        let tx_hash = generate_hash();
        let account_sequence = 1;
        let tickets = vec![1, 2, 3, 4, 5];

        let invalid_evidences_input = vec![
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: None,
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                },
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: Some(2),
                transaction_result: TransactionResult::Rejected,
                operation_result: OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                },
            },
            Evidence::XRPLTransactionResult {
                tx_hash: None,
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                },
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                },
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Invalid,
                operation_result: OperationResult::TicketsAllocation { tickets: None },
            },
            Evidence::XRPLTransactionResult {
                tx_hash: None,
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Invalid,
                operation_result: OperationResult::TicketsAllocation {
                    tickets: Some(tickets),
                },
            },
        ];

        let expected_errors = vec![
            ContractError::InvalidTransactionResultEvidence {},
            ContractError::InvalidTransactionResultEvidence {},
            ContractError::InvalidSuccessfulTransactionResultEvidence {},
            ContractError::InvalidTicketAllocationEvidence {},
            ContractError::InvalidFailedTransactionResultEvidence {},
            ContractError::InvalidTicketAllocationEvidence {},
        ];

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        for (index, evidence) in invalid_evidences_input.iter().enumerate() {
            let invalid_evidence = wasm
                .execute::<ExecuteMsg>(
                    &contract_addr,
                    &ExecuteMsg::SaveEvidence {
                        evidence: evidence.clone(),
                    },
                    &[],
                    &signer,
                )
                .unwrap_err();

            assert!(invalid_evidence
                .to_string()
                .contains(expected_errors[index].to_string().as_str()));
        }
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
        let relayer = Relayer {
            coreum_address: Addr::unchecked(signer.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer],
            1,
            50,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
        );

        // Try transfering from user that is not owner, should fail
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

        // Try registering a coreum token as not_owner, should fail
        let register_coreum_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "any_denom".to_string(),
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

        // Try registering an XRPL token as not_owner, should fail
        let register_xrpl_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: generate_xrpl_address(),
                    currency: "USD".to_string(),
                    sending_precision: 4,
                    max_holding_amount: Uint128::new(50000),
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

        // Trying to send from an address that is not a relayer should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: generate_xrpl_address(),
                        currency: "USD".to_string(),
                        amount: Uint128::new(100),
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

        // Try recovering tickets as not_owner, should fail
        let recover_tickets = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverTickets {
                    account_sequence: 1,
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
            tx_hash: "any_hash".to_string(),
            issuer: "any_issuer".to_string(),
            currency: "any_currency".to_string(),
            amount: Uint128::new(100),
            recipient: Addr::unchecked("signer"),
        };

        let evidence2 = Evidence::XRPLToCoreumTransfer {
            tx_hash: "any_hash".to_string(),
            issuer: "any_issuer".to_string(),
            currency: "any_currency".to_string(),
            amount: Uint128::new(101),
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
            tx_hash: Some("any_hash123".to_string()),
            account_sequence: Some(1),
            ticket_sequence: None,
            transaction_result: TransactionResult::Rejected,
            operation_result: OperationResult::TicketsAllocation {
                tickets: Some(vec![1, 2, 3, 4, 5]),
            },
        };

        let evidence4 = Evidence::XRPLTransactionResult {
            tx_hash: Some("any_hash123".to_string()),
            account_sequence: Some(1),
            ticket_sequence: None,
            transaction_result: TransactionResult::Accepted,
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
