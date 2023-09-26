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
    const XRP_SYMBOL: &str = "xrp";

    fn store_and_instantiate(
        wasm: &Wasm<'_, CoreumTestApp>,
        signer: &SigningAccount,
        admin: Addr,
        relayers: Vec<Addr>,
        evidence_threshold: u32,
        max_used_tickets: u32,
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
                admin,
                relayers,
                evidence_threshold,
                max_used_tickets,
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
            50,
        );
        assert!(!contract_addr.is_empty());

        // We check that trying to instantiate with invalid issue fee fails.
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    admin: Addr::unchecked(signer.address()),
                    relayers: vec![Addr::unchecked(signer.address())],
                    evidence_threshold: 1,
                    max_used_tickets: 50,
                },
                None,
                "label".into(),
                &coins(10, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        // We query the issued token by the contract instantiation (XRP)
        let query_response = assetft
            .query_tokens(&QueryTokensRequest {
                pagination: None,
                issuer: contract_addr.clone(),
            })
            .unwrap();

        assert!(error
            .to_string()
            .contains("Need to send exactly the issue fee amount"));

        assert_eq!(
            query_response.tokens[0],
            Token {
                denom: format!("{}-{}", XRP_SYMBOL, contract_addr.to_lowercase()),
                issuer: contract_addr.clone(),
                symbol: XRP_SYMBOL.to_string(),
                subunit: XRP_SYMBOL.to_string(),
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

        let new_admin = app
            .init_account(&coins(100_000_000_000, FEE_DENOM))
            .unwrap();
        let wasm = Wasm::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![Addr::unchecked(signer.address())],
            1,
            50,
        );

        //Query current admin
        let query_admin = wasm
            .query::<QueryMsg, cw_ownable::Ownership<String>>(
                &contract_addr,
                &QueryMsg::Ownership {},
            )
            .unwrap();

        assert_eq!(query_admin.owner.unwrap(), signer.address().to_string());

        // Current admin is going to transfer ownership to another address (new_admin)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateOwnership(cw_ownable::Action::TransferOwnership {
                new_owner: new_admin.address(),
                expiry: None,
            }),
            &vec![],
            &signer,
        )
        .unwrap();

        // New admin is going to accept the ownership
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateOwnership(cw_ownable::Action::AcceptOwnership {}),
            &vec![],
            &new_admin,
        )
        .unwrap();

        let query_admin = wasm
            .query::<QueryMsg, cw_ownable::Ownership<String>>(
                &contract_addr,
                &QueryMsg::Ownership {},
            )
            .unwrap();

        assert_eq!(query_admin.owner.unwrap(), new_admin.address().to_string());
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
            50,
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
            50,
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
            format!("{}-{}", XRP_SYMBOL, &contract_addr.to_lowercase())
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
            50,
        );

        let query_xrpl_token = wasm
            .query::<QueryMsg, XrplTokenResponse>(
                &contract_addr,
                &QueryMsg::XrplToken {
                    issuer: XRP_SYMBOL.to_string(),
                    currency: XRP_SYMBOL.to_string(),
                },
            )
            .unwrap();
        assert_eq!(
            query_xrpl_token.token.coreum_denom,
            format!("{}-{}", XRP_SYMBOL, &contract_addr.to_lowercase())
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
            50,
        );

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: "random_denom".to_string(),
                decimals: 6,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        //Query 1 token
        let query_coreum_token = wasm
            .query::<QueryMsg, CoreumTokenResponse>(
                &contract_addr,
                &QueryMsg::CoreumToken {
                    denom: "random_denom".to_string(),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_token.token.xrpl_currency.len(), 10);

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
        assert_eq!(query_coreum_tokens.tokens.len(), 1);
        assert_eq!(query_coreum_tokens.tokens[0].denom, "random_denom".to_string());
    }
}
