#[cfg(test)]
mod tests {
    use coreum_test_tube::{Account, AssetFT, CoreumTestApp, Module, SigningAccount, Wasm};
    use coreum_wasm_sdk::{
        assetft::{BURNING, IBC, MINTING},
        types::coreum::asset::ft::v1::{QueryBalanceRequest, QueryTokensRequest, Token},
    };
    use cosmwasm_std::{coin, coins, Addr, Uint128};

    use crate::{
        error::ContractError,
        msg::{
            CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
            XRPLTokenResponse, XRPLTokensResponse,
        },
        state::Config,
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
            },
            None,
            "label".into(),
            &coins(10_000_000, FEE_DENOM),
            &signer,
        )
        .unwrap()
        .data
        .address
    }

    #[test]
    fn contract_instantiation() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&[coin(100_000_000_000, FEE_DENOM)])
            .unwrap();

        let wasm = Wasm::new(&app);
        let assetft = AssetFT::new(&app);

        //We check that we can store and instantiate
        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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
                },
                None,
                "label".into(),
                &coins(10, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::InvalidIssueFee {}.to_string().as_str()));

        // We query the issued token by the contract instantiation (XRP)
        let query_response = assetft
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
        );

        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();
        assert_eq!(query_config.evidence_threshold, 1);
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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
    fn query_xrpl_token() {
        let app = CoreumTestApp::new();
        let signer = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();

        let wasm = Wasm::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
        );

        let query_xrpl_token = wasm
            .query::<QueryMsg, XRPLTokenResponse>(
                &contract_addr,
                &QueryMsg::XRPLToken {
                    issuer: None,
                    currency: None,
                },
            )
            .unwrap();
        assert_eq!(
            query_xrpl_token.token.coreum_denom,
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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
            .contains(ContractError::InvalidIssueFee {}.to_string().as_str()));

        //Register two tokens correctly
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer,
                    currency: token.currency,
                },
                &coins(10_000_000, FEE_DENOM),
                &signer,
            )
            .unwrap();
        }

        // Check tokens are in the bank module
        let assetft = AssetFT::new(&app);
        let query_response = assetft
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
                &coins(10_000_000, FEE_DENOM),
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

        //Query 1 token
        let query_token = wasm
            .query::<QueryMsg, XRPLTokenResponse>(
                &contract_addr,
                &QueryMsg::XRPLToken {
                    issuer: Some(test_tokens[0].issuer.clone()),
                    currency: Some(test_tokens[0].currency.clone()),
                },
            )
            .unwrap();
        assert!(query_token
            .token
            .coreum_denom
            .ends_with(contract_addr.to_lowercase().as_str()));

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
            query_xrpl_tokens.tokens[0].issuer.clone().unwrap(),
            test_tokens[0].issuer
        );
        assert_eq!(
            query_xrpl_tokens.tokens[0].currency.clone().unwrap(),
            test_tokens[0].currency
        );
        assert!(query_xrpl_tokens.tokens[1]
            .coreum_denom
            .starts_with(XRPL_DENOM_PREFIX));
        assert_eq!(
            query_xrpl_tokens.tokens[1].issuer.clone().unwrap(),
            test_tokens[1].issuer
        );
        assert_eq!(
            query_xrpl_tokens.tokens[1].currency.clone().unwrap(),
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
        let assetft = AssetFT::new(&app);

        //Test with 1 relayer and 1 evidence threshold first
        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(relayer1.address())],
            1,
        );

        let test_tokens = vec![XRPLToken {
            issuer: "issuer1".to_string(),
            currency: "currency1".to_string(),
        }];

        //Register a token
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer,
                    currency: token.currency,
                },
                &coins(10_000_000, FEE_DENOM),
                signer,
            )
            .unwrap();
        }

        let query_xrpl_token = wasm
            .query::<QueryMsg, XRPLTokenResponse>(
                &contract_addr,
                &QueryMsg::XRPLToken {
                    issuer: Some(test_tokens[0].issuer.clone()),
                    currency: Some(test_tokens[0].currency.clone()),
                },
            )
            .unwrap();

        let denom = query_xrpl_token.token.coreum_denom;
        let hash = "random_hash".to_string();
        let amount = Uint128::from(100 as u128);

        //Bridge with 1 relayer should immediately mint and send to the receiver address
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendFromXRPLToCoreum {
                hash: hash.clone(),
                issuer: test_tokens[0].issuer.clone(),
                currency: test_tokens[0].currency.clone(),
                amount: amount.clone(),
                recipient: Addr::unchecked(receiver.address()),
            },
            &[],
            relayer1,
        )
        .unwrap();

        let request_balance = assetft
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
        );

        //Register a token
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer,
                    currency: token.currency,
                },
                &coins(10_000_000, FEE_DENOM),
                signer,
            )
            .unwrap();
        }

        let query_xrpl_token = wasm
            .query::<QueryMsg, XRPLTokenResponse>(
                &contract_addr,
                &QueryMsg::XRPLToken {
                    issuer: Some(test_tokens[0].issuer.clone()),
                    currency: Some(test_tokens[0].currency.clone()),
                },
            )
            .unwrap();

        let denom = query_xrpl_token.token.coreum_denom;

        //Trying to send from an address that is not a relayer should fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendFromXRPLToCoreum {
                    hash: hash.clone(),
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
                &[],
                signer,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::UnauthorizedOperation {}.to_string().as_str()));

        //Trying to send a token that is not previously registered should also fail
        let relayer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendFromXRPLToCoreum {
                    hash: hash.clone(),
                    issuer: "not_registered".to_string(),
                    currency: "not_registered".to_string(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
                },
                &[],
                relayer1,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        //First relayer to execute should not trigger a mint and send
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendFromXRPLToCoreum {
                hash: hash.clone(),
                issuer: test_tokens[0].issuer.clone(),
                currency: test_tokens[0].currency.clone(),
                amount: amount.clone(),
                recipient: Addr::unchecked(receiver.address()),
            },
            &[],
            relayer1,
        )
        .unwrap();

        //Balance should be 0
        let request_balance = assetft
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
                &ExecuteMsg::SendFromXRPLToCoreum {
                    hash: hash.clone(),
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
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
            &ExecuteMsg::SendFromXRPLToCoreum {
                hash: hash.clone(),
                issuer: test_tokens[0].issuer.clone(),
                currency: test_tokens[0].currency.clone(),
                amount: amount.clone(),
                recipient: Addr::unchecked(receiver.address()),
            },
            &[],
            relayer2,
        )
        .unwrap();

        //Balance should be 0
        let request_balance = assetft
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
                &ExecuteMsg::SendFromXRPLToCoreum {
                    hash: hash.clone(),
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    amount: amount.clone(),
                    recipient: Addr::unchecked(receiver.address()),
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

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
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
                &coins(10_000_000, FEE_DENOM),
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
                &ExecuteMsg::SendFromXRPLToCoreum {
                    hash: "random_hash".to_string(),
                    issuer: "random_issuer".to_string(),
                    currency: "random_currency".to_string(),
                    amount: Uint128::from(100 as u128),
                    recipient: Addr::unchecked(signer.address()),
                },
                &[],
                &not_owner,
            )
            .unwrap_err();

        assert!(relayer_error
            .to_string()
            .contains(ContractError::UnauthorizedOperation {}.to_string().as_str()));
    }
}
