#[cfg(test)]
mod tests {
    use coreum_test_tube::{Account, AssetFT, CoreumTestApp, Module, SigningAccount, Wasm};
    use coreum_wasm_sdk::{
        assetft::{BURNING, IBC, MINTING},
        types::coreum::asset::ft::v1::{QueryTokensRequest, Token},
    };
    use cosmwasm_std::{coin, coins, Addr};

    use crate::{
        msg::{
            CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
            XrplTokenResponse, XrplTokensResponse,
        },
        state::Config,
    };
    const FEE_DENOM: &str = "ucore";
    const XRP_SYMBOL: &str = "XRL";
    const XRP_SUBUNIT: &str = "drop";
    const COREUM_CURRENCY_PREFIX: &str = "coreum";
    const XRPL_DENOM_PREFIX: &str = "xrpl";

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
            .contains("Need to send exactly the issue fee amount"));

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

        assert!(transfer_error
            .to_string()
            .contains("Caller is not the contract's current owner"));
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
            .query::<QueryMsg, XrplTokensResponse>(
                &contract_addr,
                &QueryMsg::XrplTokens {
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
            .query::<QueryMsg, XrplTokenResponse>(
                &contract_addr,
                &QueryMsg::XrplToken {
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

        let test_denom = "random_denom".to_string();
        let test_denom2 = "random_denom2".to_string();

        //Register 2 tokens
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: test_denom.clone(),
                decimals: 6,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: test_denom2.clone(),
                decimals: 6,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        //Register 1 token with same denom, should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: test_denom.clone(),
                    decimals: 6,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(register_error
            .to_string()
            .contains(format!("Token {} already registered", test_denom).as_str()));

        //Query 1 token
        let query_coreum_token = wasm
            .query::<QueryMsg, CoreumTokenResponse>(
                &contract_addr,
                &QueryMsg::CoreumToken {
                    denom: test_denom.clone(),
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
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_denom);

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
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_denom);

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
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_denom2);
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

        let test_issuer1 = "issuer1".to_string();
        let test_issuer2 = "issuer2".to_string();
        let test_currency1 = "currency1".to_string();
        let test_currency2 = "currency2".to_string();

        //Register token with incorrect fee (too much), should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_issuer1.clone(),
                    currency: test_currency1.clone(),
                    decimals: 6,
                },
                &coins(20_000_000, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(register_error
            .to_string()
            .contains("Need to send exactly the issue fee amount"));

        //Register two tokens correctly
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_issuer1.clone(),
                currency: test_currency1.clone(),
                decimals: 6,
            },
            &coins(10_000_000, FEE_DENOM),
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_issuer2.clone(),
                currency: test_currency2.clone(),
                decimals: 6,
            },
            &coins(10_000_000, FEE_DENOM),
            &signer,
        )
        .unwrap();

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
                    issuer: test_issuer1.clone(),
                    currency: test_currency1.clone(),
                    decimals: 6,
                },
                &coins(10_000_000, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(register_error.to_string().contains(
            format!(
                "Token with issuer: {} and currency: {} is already registered",
                test_issuer1, test_currency1
            )
            .as_str()
        ));

        //Query 1 token
        let query_token = wasm
            .query::<QueryMsg, XrplTokenResponse>(
                &contract_addr,
                &QueryMsg::XrplToken {
                    issuer: Some(test_issuer1.clone()),
                    currency: Some(test_currency1.clone()),
                },
            )
            .unwrap();
        assert!(query_token
            .token
            .coreum_denom
            .ends_with(contract_addr.to_lowercase().as_str()));

        //Query all tokens
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XrplTokensResponse>(
                &contract_addr,
                &QueryMsg::XrplTokens {
                    offset: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 3);

        //Query all tokens with limit
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XrplTokensResponse>(
                &contract_addr,
                &QueryMsg::XrplTokens {
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
            .query::<QueryMsg, XrplTokensResponse>(
                &contract_addr,
                &QueryMsg::XrplTokens {
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
            test_issuer1
        );
        assert_eq!(
            query_xrpl_tokens.tokens[0].currency.clone().unwrap(),
            test_currency1
        );
        assert!(query_xrpl_tokens.tokens[1]
            .coreum_denom
            .starts_with(XRPL_DENOM_PREFIX));
        assert_eq!(
            query_xrpl_tokens.tokens[1].issuer.clone().unwrap(),
            test_issuer2
        );
        assert_eq!(
            query_xrpl_tokens.tokens[1].currency.clone().unwrap(),
            test_currency2
        );
    }
}
