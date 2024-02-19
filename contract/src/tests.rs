#[cfg(test)]
mod tests {
    use coreum_test_tube::{Account, AssetFT, Bank, CoreumTestApp, Module, SigningAccount, Wasm};
    use coreum_wasm_sdk::types::coreum::asset::ft::v1::{MsgFreeze, MsgUnfreeze};
    use coreum_wasm_sdk::types::cosmos::bank::v1beta1::QueryTotalSupplyRequest;
    use coreum_wasm_sdk::types::cosmos::base::v1beta1::Coin as BaseCoin;
    use coreum_wasm_sdk::{
        assetft::{BURNING, FREEZING, IBC, MINTING},
        types::{
            coreum::asset::ft::v1::{
                MsgIssue, QueryBalanceRequest, QueryParamsRequest, QueryTokensRequest, Token,
            },
            cosmos::bank::v1beta1::MsgSend,
        },
    };
    use cosmwasm_std::{coin, coins, Addr, Coin, Uint128};
    use rand::{distributions::Alphanumeric, thread_rng, Rng};
    use ripple_keypairs::Seed;
    use sha2::{Digest, Sha256};
    use std::collections::HashMap;

    use crate::address::validate_xrpl_address;
    use crate::contract::{INITIAL_PROHIBITED_XRPL_RECIPIENTS, MAX_RELAYERS};
    use crate::msg::{
        BridgeStateResponse, ProcessedTxsResponse, ProhibitedXRPLRecipientsResponse,
        TransactionEvidence, TransactionEvidencesResponse,
    };
    use crate::state::BridgeState;
    use crate::{
        contract::{XRP_CURRENCY, XRP_ISSUER},
        error::ContractError,
        evidence::{Evidence, OperationResult, TransactionResult},
        msg::{
            AvailableTicketsResponse, CoreumTokensResponse, ExecuteMsg, FeesCollectedResponse,
            InstantiateMsg, PendingOperationsResponse, PendingRefundsResponse, QueryMsg,
            XRPLTokensResponse,
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
        pub max_holding_amount: Uint128,
        pub bridging_fee: Uint128,
    }

    #[derive(Clone)]
    struct CoreumToken {
        pub denom: String,
        pub decimals: u32,
        pub sending_precision: i32,
        pub max_holding_amount: Uint128,
        pub bridging_fee: Uint128,
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
        bridge_xrpl_address: String,
        xrpl_base_fee: u64,
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
                bridge_xrpl_address,
                xrpl_base_fee,
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
        let seed = Seed::random();
        let (_, public_key) = seed.derive_keypair().unwrap();
        let address = public_key.derive_address();
        address
    }

    pub fn generate_invalid_xrpl_address() -> String {
        let mut address = 'r'.to_string();
        let mut rand = String::from_utf8(
            thread_rng()
                .sample_iter(&Alphanumeric)
                .take(30)
                .collect::<Vec<_>>(),
        )
        .unwrap();

        rand = rand.replace("0", "1");
        rand = rand.replace("O", "o");
        rand = rand.replace("I", "i");
        rand = rand.replace("l", "L");

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
            xrpl_address,
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
            generate_xrpl_address(),
            10,
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
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::DuplicatedRelayer {}.to_string().as_str()));

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
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::DuplicatedRelayer {}.to_string().as_str()));

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
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::DuplicatedRelayer {}.to_string().as_str()));

        // We check that trying to instantiate with invalid bridge_xrpl_address fails
        let invalid_address = "rf0BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn".to_string(); //invalid because contains a 0
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                    bridge_xrpl_address: invalid_address.clone(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &coins(10, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(error.to_string().contains(
            ContractError::InvalidXRPLAddress {
                address: invalid_address
            }
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
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
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
                    relayers: vec![relayer.clone()],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 1,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
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

        // Instantiating with threshold 0 will fail
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![],
                    evidence_threshold: 0,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::InvalidThreshold {}.to_string().as_str()));

        // Instantiating with too many relayers (> 32) should fail
        let mut too_many_relayers = vec![];
        for _ in 0..MAX_RELAYERS + 1 {
            let coreum_address = app.init_account(&vec![]).unwrap().address();
            too_many_relayers.push(Relayer {
                coreum_address: Addr::unchecked(coreum_address),
                xrpl_address: generate_xrpl_address(),
                xrpl_pub_key: generate_xrpl_pub_key(),
            });
        }

        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: too_many_relayers,
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::TooManyRelayers {}.to_string().as_str()));

        // We check that trying to instantiate with an invalid trust set amount will fail
        let error = wasm
            .instantiate(
                1,
                &InstantiateMsg {
                    owner: Addr::unchecked(signer.address()),
                    relayers: vec![relayer, relayer_correct],
                    evidence_threshold: 1,
                    used_ticket_sequence_threshold: 50,
                    trust_set_limit_amount: Uint128::new(10000000000000001),
                    bridge_xrpl_address: generate_xrpl_address(),
                    xrpl_base_fee: 10,
                },
                None,
                "label".into(),
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(error
            .to_string()
            .contains(ContractError::InvalidXRPLAmount {}.to_string().as_str()));

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
                uri: "".to_string(),
                uri_hash: "".to_string(),
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
            generate_xrpl_address(),
            10,
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
    fn queries() {
        let app = CoreumTestApp::new();
        let accounts_number = 4;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let xrpl_addresses: Vec<String> = (0..3).map(|_| generate_xrpl_address()).collect();
        let xrpl_pub_keys: Vec<String> = (0..3).map(|_| generate_xrpl_pub_key()).collect();

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

        let bridge_xrpl_address = generate_xrpl_address();
        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![
                relayers[0].clone(),
                relayers[1].clone(),
                relayers[2].clone(),
            ],
            3,
            5,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            10,
        );

        // Query the config
        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(
            query_config,
            Config {
                relayers: vec![
                    relayers[0].clone(),
                    relayers[1].clone(),
                    relayers[2].clone()
                ],
                evidence_threshold: 3,
                used_ticket_sequence_threshold: 5,
                trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                bridge_xrpl_address: bridge_xrpl_address.clone(),
                bridge_state: BridgeState::Active,
                xrpl_base_fee: 10,
            }
        );

        // Query XRPL tokens
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
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
                bridging_fee: Uint128::zero(),
            }
        );

        // Let's create a ticket operation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(6),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Two relayers will return the evidence as rejected and one as accepted
        let tx_hash = generate_hash();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..7).collect()),
                    }),
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
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..7).collect()),
                    }),
                },
            },
            &vec![],
            relayer_accounts[1],
        )
        .unwrap();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Rejected,
                    operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
                },
            },
            &vec![],
            relayer_accounts[2],
        )
        .unwrap();

        // Let's query all the transaction evidences (we should get two)
        let query_transaction_evidences = wasm
            .query::<QueryMsg, TransactionEvidencesResponse>(
                &contract_addr,
                &QueryMsg::TransactionEvidences {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_transaction_evidences.transaction_evidences.len(), 2);

        // Let's query all the transaction evidences with pagination
        let query_transaction_evidences = wasm
            .query::<QueryMsg, TransactionEvidencesResponse>(
                &contract_addr,
                &QueryMsg::TransactionEvidences {
                    start_after_key: None,
                    limit: Some(1),
                },
            )
            .unwrap();

        assert_eq!(query_transaction_evidences.transaction_evidences.len(), 1);

        let query_transaction_evidences = wasm
            .query::<QueryMsg, TransactionEvidencesResponse>(
                &contract_addr,
                &QueryMsg::TransactionEvidences {
                    start_after_key: query_transaction_evidences.last_key,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_transaction_evidences.transaction_evidences.len(), 1);

        // Let's query a transaction evidences by evidence hash and verify that we have an address that provided that evidence
        let query_transaction_evidence = wasm
            .query::<QueryMsg, TransactionEvidence>(
                &contract_addr,
                &QueryMsg::TransactionEvidence {
                    hash: query_transaction_evidences.transaction_evidences[0]
                        .hash
                        .clone(),
                },
            )
            .unwrap();

        assert!(!query_transaction_evidence.relayer_addresses.is_empty());

        // Let's query the prohibited recipients
        let query_prohibited_recipients = wasm
            .query::<QueryMsg, ProhibitedXRPLRecipientsResponse>(
                &contract_addr,
                &QueryMsg::ProhibitedXRPLRecipients {},
            )
            .unwrap();

        assert_eq!(
            query_prohibited_recipients.prohibited_xrpl_recipients.len(),
            INITIAL_PROHIBITED_XRPL_RECIPIENTS.len() + 1
        );
        assert!(query_prohibited_recipients
            .prohibited_xrpl_recipients
            .contains(&bridge_xrpl_address));

        // Let's try to update this by adding a new one and query again
        let new_prohibited_recipient = generate_xrpl_address();
        let mut prohibited_recipients = query_prohibited_recipients
            .prohibited_xrpl_recipients
            .clone();
        prohibited_recipients.push(new_prohibited_recipient.clone());
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateProhibitedXRPLRecipients {
                prohibited_xrpl_recipients: prohibited_recipients,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_prohibited_recipients = wasm
            .query::<QueryMsg, ProhibitedXRPLRecipientsResponse>(
                &contract_addr,
                &QueryMsg::ProhibitedXRPLRecipients {},
            )
            .unwrap();

        assert_eq!(
            query_prohibited_recipients.prohibited_xrpl_recipients.len(),
            INITIAL_PROHIBITED_XRPL_RECIPIENTS.len() + 2
        );
        assert!(query_prohibited_recipients
            .prohibited_xrpl_recipients
            .contains(&bridge_xrpl_address));

        assert!(query_prohibited_recipients
            .prohibited_xrpl_recipients
            .contains(&new_prohibited_recipient));

        // If we try to update this from an account that is not the owner it will fail
        let update_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateProhibitedXRPLRecipients {
                    prohibited_xrpl_recipients: vec![],
                },
                &vec![],
                &relayer_accounts[0],
            )
            .unwrap_err();

        assert!(update_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));
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
            generate_xrpl_address(),
            10,
        );

        let test_tokens = vec![
            CoreumToken {
                denom: "denom1".to_string(),
                decimals: 6,
                sending_precision: 6,
                max_holding_amount: Uint128::new(100000),
                bridging_fee: Uint128::zero(),
            },
            CoreumToken {
                denom: "denom2".to_string(),
                decimals: 6,
                sending_precision: 6,
                max_holding_amount: Uint128::new(100000),
                bridging_fee: Uint128::zero(),
            },
        ];

        // Register two tokens correctly
        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: token.denom,
                    decimals: token.decimals,
                    sending_precision: token.sending_precision,
                    max_holding_amount: token.max_holding_amount,
                    bridging_fee: token.bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap();
        }

        // Registering a token with same denom, should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: test_tokens[0].denom.clone(),
                    decimals: 6,
                    sending_precision: 6,
                    max_holding_amount: Uint128::one(),
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(register_error.to_string().contains(
            ContractError::CoreumTokenAlreadyRegistered {
                denom: test_tokens[0].denom.clone()
            }
            .to_string()
            .as_str()
        ));

        // Registering a token with invalid sending precision should fail
        let register_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: test_tokens[0].denom.clone(),
                    decimals: 6,
                    sending_precision: -17,
                    max_holding_amount: Uint128::one(),
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(register_error.to_string().contains(
            ContractError::InvalidSendingPrecision {}
                .to_string()
                .as_str()
        ));

        // Registering tokens with invalid denoms will fail
        let invalid_denom_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "1aa".to_string(), // Starts with a number
                    decimals: test_tokens[0].decimals,
                    sending_precision: test_tokens[0].sending_precision,
                    max_holding_amount: test_tokens[0].max_holding_amount,
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(invalid_denom_error
            .to_string()
            .contains(ContractError::InvalidDenom {}.to_string().as_str()));

        let invalid_denom_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "aa".to_string(), // Too short
                    decimals: test_tokens[0].decimals,
                    sending_precision: test_tokens[0].sending_precision,
                    max_holding_amount: test_tokens[0].max_holding_amount,
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(invalid_denom_error
            .to_string()
            .contains(ContractError::InvalidDenom {}.to_string().as_str()));

        let invalid_denom_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa".to_string(), // Too long
                    decimals: test_tokens[0].decimals,
                    sending_precision: test_tokens[0].sending_precision,
                    max_holding_amount: test_tokens[0].max_holding_amount,
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(invalid_denom_error
            .to_string()
            .contains(ContractError::InvalidDenom {}.to_string().as_str()));

        let invalid_denom_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "aa$".to_string(), // Invalid symbols
                    decimals: test_tokens[0].decimals,
                    sending_precision: test_tokens[0].sending_precision,
                    max_holding_amount: test_tokens[0].max_holding_amount,
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(invalid_denom_error
            .to_string()
            .contains(ContractError::InvalidDenom {}.to_string().as_str()));

        // Query all tokens
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 2);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[0].denom);
        assert_eq!(query_coreum_tokens.tokens[1].denom, test_tokens[1].denom);
        assert_eq!(
            query_coreum_tokens.tokens[0].xrpl_currency,
            query_coreum_tokens.tokens[0].xrpl_currency.to_uppercase()
        );
        assert_eq!(
            query_coreum_tokens.tokens[1].xrpl_currency,
            query_coreum_tokens.tokens[1].xrpl_currency.to_uppercase()
        );

        // Query tokens with limit
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 1);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[0].denom);

        // Query tokens with pagination
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: query_coreum_tokens.last_key,
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 1);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[1].denom);
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
            generate_xrpl_address(),
            10,
        );

        let test_tokens = vec![
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "USD".to_string(),     // Valid standard currency code
                sending_precision: -15,
                max_holding_amount: Uint128::new(100),
                bridging_fee: Uint128::zero(),
            },
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "015841551A748AD2C1F76FF6ECB0CCCD00000000".to_string(), // Valid hexadecimal currency
                sending_precision: 15,
                max_holding_amount: Uint128::new(50000),
                bridging_fee: Uint128::zero(),
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
                    max_holding_amount: test_tokens[0].max_holding_amount.clone(),
                    bridging_fee: test_tokens[0].bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(issuer_error.to_string().contains(
            ContractError::InvalidXRPLAddress {
                address: "not_valid_issuer".to_string()
            }
            .to_string()
            .as_str()
        ));

        // Registering a token with an invalid precision should fail.
        let issuer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: -16,
                    max_holding_amount: test_tokens[0].max_holding_amount.clone(),
                    bridging_fee: test_tokens[0].bridging_fee,
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
                    issuer: test_tokens[0].issuer.clone(),
                    currency: test_tokens[0].currency.clone(),
                    sending_precision: 16,
                    max_holding_amount: test_tokens[0].max_holding_amount.clone(),
                    bridging_fee: test_tokens[0].bridging_fee,
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
                    max_holding_amount: test_tokens[1].max_holding_amount.clone(),
                    bridging_fee: test_tokens[1].bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(currency_error
            .to_string()
            .contains(ContractError::InvalidXRPLCurrency {}.to_string().as_str()));

        // Registering a token with an invalid symbol should fail
        let currency_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: "US~".to_string(),
                    sending_precision: test_tokens[1].sending_precision.clone(),
                    max_holding_amount: test_tokens[1].max_holding_amount.clone(),
                    bridging_fee: test_tokens[1].bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(currency_error
            .to_string()
            .contains(ContractError::InvalidXRPLCurrency {}.to_string().as_str()));

        // Registering a token with an invalid hexadecimal currency (not uppercase) should fail
        let currency_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: "015841551A748AD2C1f76FF6ECB0CCCD00000000".to_string(),
                    sending_precision: test_tokens[1].sending_precision.clone(),
                    max_holding_amount: test_tokens[1].max_holding_amount.clone(),
                    bridging_fee: test_tokens[1].bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(currency_error
            .to_string()
            .contains(ContractError::InvalidXRPLCurrency {}.to_string().as_str()));

        // Registering a token with an invalid hexadecimal currency (starting with 0x00) should fail
        let currency_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: "005841551A748AD2C1F76FF6ECB0CCCD00000000".to_string(),
                    sending_precision: test_tokens[1].sending_precision.clone(),
                    max_holding_amount: test_tokens[1].max_holding_amount.clone(),
                    bridging_fee: test_tokens[1].bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(currency_error
            .to_string()
            .contains(ContractError::InvalidXRPLCurrency {}.to_string().as_str()));

        // Registering a token with an "XRP" as currency should fail
        let currency_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: test_tokens[1].issuer.clone(),
                    currency: "XRP".to_string(),
                    sending_precision: test_tokens[1].sending_precision.clone(),
                    max_holding_amount: test_tokens[1].max_holding_amount.clone(),
                    bridging_fee: test_tokens[1].bridging_fee,
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
                    max_holding_amount: test_tokens[0].max_holding_amount.clone(),
                    bridging_fee: test_tokens[0].bridging_fee,
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
                    max_holding_amount: test_tokens[0].max_holding_amount,
                    bridging_fee: test_tokens[0].bridging_fee,
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
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
                    max_holding_amount: token.max_holding_amount,
                    bridging_fee: token.bridging_fee,
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
            max_holding_amount: Uint128::new(100),
            bridging_fee: Uint128::zero(),
        };

        let last_ticket_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: extra_token.issuer,
                    currency: extra_token.currency,
                    sending_precision: extra_token.sending_precision,
                    max_holding_amount: extra_token.max_holding_amount,
                    bridging_fee: extra_token.bridging_fee,
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
                    max_holding_amount: test_tokens[0].max_holding_amount.clone(),
                    bridging_fee: test_tokens[0].bridging_fee,
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
                    start_after_key: None,
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
                    start_after_key: None,
                    limit: Some(1),
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 1);

        // Query all tokens with pagination
        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: query_xrpl_tokens.last_key,
                    limit: Some(2),
                },
            )
            .unwrap();
        assert_eq!(query_xrpl_tokens.tokens.len(), 2);
    }

    #[test]
    fn send_xrpl_originated_tokens_from_xrpl_to_coreum() {
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
            generate_xrpl_address(),
            10,
        );

        let test_token = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "USD".to_string(),
            sending_precision: 15,
            max_holding_amount: Uint128::new(50000),
            bridging_fee: Uint128::zero(),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
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
                max_holding_amount: test_token.max_holding_amount.clone(),
                bridging_fee: test_token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
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
            .contains(ContractError::TokenNotEnabled {}.to_string().as_str()));

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
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

        // If we try to bridge to the contract address, it should fail
        let bridge_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token.issuer.clone(),
                        currency: test_token.currency.clone(),
                        amount: amount.clone(),
                        recipient: Addr::unchecked(contract_addr.clone()),
                    },
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(bridge_error
            .to_string()
            .contains(ContractError::ProhibitedRecipient {}.to_string().as_str()));

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
            generate_xrpl_address(),
            10,
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
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
                max_holding_amount: test_token.max_holding_amount,
                bridging_fee: test_token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
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
                    operation_result: None,
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
                    start_after_key: None,
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
    fn send_coreum_originated_tokens_from_xrpl_to_coreum() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let sender = accounts.get(1).unwrap();
        let relayer_account = accounts.get(2).unwrap();
        let relayer = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let xrpl_receiver_address = generate_xrpl_address();
        let bridge_xrpl_address = generate_xrpl_address();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            9,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            10,
        );

        // Add enough tickets for all our test operations

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(10),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..11).collect()),
                    }),
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Let's issue a token where decimals are less than an XRPL token decimals to the sender and register it.
        let asset_ft = AssetFT::new(&app);
        let symbol = "TEST".to_string();
        let subunit = "utest".to_string();
        let decimals = 6;
        let initial_amount = Uint128::new(100000000000000000000);
        asset_ft
            .issue(
                MsgIssue {
                    issuer: signer.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32, FREEZING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &signer,
            )
            .unwrap();

        let denom = format!("{}-{}", subunit, signer.address()).to_lowercase();

        // Send all initial amount tokens to the sender so that we can correctly test freezing without sending to the issuer
        let bank = Bank::new(&app);
        bank.send(
            MsgSend {
                from_address: signer.address(),
                to_address: sender.address(),
                amount: vec![BaseCoin {
                    amount: initial_amount.to_string(),
                    denom: denom.to_string(),
                }],
            },
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: denom.clone(),
                decimals,
                sending_precision: 5,
                max_holding_amount: Uint128::new(100000000000000000000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // It should truncate 1 because sending precision is 5
        let amount_to_send = Uint128::new(1000001);

        // If we try to send an amount in the optional field it should fail.
        let send_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(Uint128::new(100)),
                },
                &coins(amount_to_send.u128(), denom.clone()),
                &sender,
            )
            .unwrap_err();

        assert!(send_error.to_string().contains(
            ContractError::DeliverAmountIsProhibited {}
                .to_string()
                .as_str()
        ));

        // If we try to send an amount that will become an invalid XRPL amount, it should fail
        let send_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: None,
                },
                &coins(10000000000000000010, denom.clone()), // Nothing is truncated, and after transforming into XRPL amount it will have more than 17 digits
                &sender,
            )
            .unwrap_err();

        assert!(send_error
            .to_string()
            .contains(ContractError::InvalidXRPLAmount {}.to_string().as_str()));

        // Try to bridge the token to the xrpl receiver address so that we can send it back.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        // Check balance of sender and contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send)
                .unwrap()
                .to_string()
        );

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount_to_send.to_string());

        // Get the token information
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let coreum_originated_token = query_coreum_tokens
            .tokens
            .iter()
            .find(|t| t.denom == denom)
            .unwrap();

        // Confirm the operation to remove it from pending operations.
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let amount_truncated_and_converted = Uint128::new(1000000000000000); // 100001 -> truncate -> 100000 -> convert -> 1e15
        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0].operation_type,
            OperationType::CoreumToXRPLTransfer {
                issuer: bridge_xrpl_address.clone(),
                currency: coreum_originated_token.xrpl_currency.clone(),
                amount: amount_truncated_and_converted,
                max_amount: Some(amount_truncated_and_converted),
                sender: Addr::unchecked(sender.address()),
                recipient: xrpl_receiver_address.clone(),
            }
        );

        let tx_hash = generate_hash();
        // Reject the operation, therefore the tokens should be stored in the pending refunds (except for truncated amount).
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Truncated amount and amount to be refunded will stay in the contract until relayers and users to be refunded claim
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, amount_to_send.to_string());

        // If we try to query pending refunds for any address that has no pending refunds, it should return an empty array
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked("any_address"),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds, vec![]);

        // Let's verify the pending refunds and try to claim them
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].xrpl_tx_hash,
            Some(tx_hash)
        );
        // Truncated amount (1) is not refundable
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(
                amount_to_send.checked_sub(Uint128::one()).unwrap().u128(),
                denom.clone()
            )
        );

        // Trying to claim a refund with an invalid pending refund operation id should fail
        let claim_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRefund {
                    pending_refund_id: "random_id".to_string(),
                },
                &[],
                &sender,
            )
            .unwrap_err();

        assert!(claim_error
            .to_string()
            .contains(ContractError::PendingRefundNotFound {}.to_string().as_str()));

        // Try to claim a pending refund with a valid pending refund operation id but not as a different user, should also fail
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &signer,
        )
        .unwrap_err();

        // Let's freeze the token to verify that claiming will fail
        asset_ft
            .freeze(
                MsgFreeze {
                    sender: signer.address(),
                    account: contract_addr.clone(),
                    coin: Some(BaseCoin {
                        denom: denom.clone(),
                        amount: "100000".to_string(),
                    }),
                },
                &signer,
            )
            .unwrap();

        // Can't claim because token is frozen
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap_err();

        // Let's unfreeze token so we can claim
        asset_ft
            .unfreeze(
                MsgUnfreeze {
                    sender: signer.address(),
                    account: contract_addr.clone(),
                    coin: Some(BaseCoin {
                        denom: denom.clone(),
                        amount: "100000".to_string(),
                    }),
                },
                &signer,
            )
            .unwrap();

        // Let's claim our pending refund
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap();

        // Verify balance of sender (to check it was correctly refunded) and verify that the amount refunded was removed from pending refunds
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(Uint128::one()) // truncated amount
                .unwrap()
                .to_string()
        );

        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // We verify our pending refund operation was removed from the pending refunds
        assert!(query_pending_refunds.pending_refunds.is_empty());

        // Try to send again
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // Send successfull evidence to remove from queue (tokens should be released on XRPL to the receiver)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 0);

        // Test sending the amount back from XRPL to Coreum
        // 10000000000 (1e10) is the minimum we can send back (15 - 5 (sending precision))
        let amount_to_send_back = Uint128::new(10000000000);

        // If we send the token with a different issuer (not multisig address) it should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: generate_xrpl_address(),
                        currency: coreum_originated_token.xrpl_currency.clone(),
                        amount: amount_to_send_back.clone(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // If we send the token with a different currency (one that is not the one in the registered token list) it should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: bridge_xrpl_address.clone(),
                        currency: "invalid_currency".to_string(),
                        amount: amount_to_send_back.clone(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // Sending under the minimum should fail (minimum - 1)
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: bridge_xrpl_address.clone(),
                        currency: coreum_originated_token.xrpl_currency.clone(),
                        amount: amount_to_send_back.checked_sub(Uint128::one()).unwrap(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Sending the right evidence should move tokens from the contract to the sender's account
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: bridge_xrpl_address.clone(),
                    currency: coreum_originated_token.xrpl_currency.clone(),
                    amount: amount_to_send_back.clone(),
                    recipient: Addr::unchecked(sender.address()),
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        // Check balance of sender and contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send) // initial amount
                .unwrap()
                .checked_sub(Uint128::one()) // amount lost during truncation of first rejection
                .unwrap()
                .checked_add(Uint128::new(10)) // Amount that we sent back (10) after conversion, the minimum
                .unwrap()
                .to_string()
        );

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            amount_to_send
                .checked_add(Uint128::one()) // Truncated amount staying in contract
                .unwrap()
                .checked_sub(Uint128::new(10))
                .unwrap()
                .to_string()
        );

        // Now let's issue a token where decimals are more than an XRPL token decimals to the sender and register it.
        let symbol = "TEST2".to_string();
        let subunit = "utest2".to_string();
        let decimals = 20;
        let initial_amount = Uint128::new(200000000000000000000); // 2e20
        asset_ft
            .issue(
                MsgIssue {
                    issuer: sender.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &sender,
            )
            .unwrap();

        let denom = format!("{}-{}", subunit, sender.address()).to_lowercase();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: denom.clone(),
                decimals,
                sending_precision: 10,
                max_holding_amount: Uint128::new(200000000000000000000), //2e20
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // It should truncate and remove all 9s because they are under precision
        let amount_to_send = Uint128::new(100000000019999999999);

        // Bridge the token to the xrpl receiver address so that we can send it back.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        // Check balance of sender and contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send)
                .unwrap()
                .to_string()
        );

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount_to_send.to_string());

        // Get the token information
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let coreum_originated_token = query_coreum_tokens
            .tokens
            .iter()
            .find(|t| t.denom == denom)
            .unwrap();

        // Confirm the operation to remove it from pending operations.
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let amount_truncated_and_converted = Uint128::new(1000000000100000); // 100000000019999999999 -> truncate -> 100000000010000000000  -> convert -> 1000000000100000
        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0].operation_type,
            OperationType::CoreumToXRPLTransfer {
                issuer: bridge_xrpl_address.clone(),
                currency: coreum_originated_token.xrpl_currency.clone(),
                amount: amount_truncated_and_converted,
                max_amount: Some(amount_truncated_and_converted),
                sender: Addr::unchecked(sender.address()),
                recipient: xrpl_receiver_address.clone(),
            }
        );

        // Reject the operation so that tokens are sent back to sender
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Truncated amount won't be sent back (goes to relayer fees) and the rest will be stored in refundable array for the user to claim
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send)
                .unwrap()
                .to_string()
        );

        // Truncated amount and refundable fees will stay in contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, amount_to_send.to_string());

        // If we query the refundable tokens that the user can claim, we should see the amount that was truncated is claimable
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // We verify that these tokens are refundable
        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(
                amount_to_send
                    .checked_sub(Uint128::new(9999999999)) // Amount truncated is not refunded to user
                    .unwrap()
                    .u128(),
                denom.clone()
            )
        );

        // Claim it, should work
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap();

        // pending refunds should now be empty
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // We verify that there are no pending refunds left
        assert!(query_pending_refunds.pending_refunds.is_empty());

        // Try to send again
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // Send successfull evidence to remove from queue (tokens should be released on XRPL to the receiver)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 0);

        // Test sending the amount back from XRPL to Coreum
        // 100000 (1e5) is the minimum we can send back (15 - 10 (sending precision))
        let amount_to_send_back = Uint128::new(100000);

        // If we send the token with a different issuer (not multisig address) it should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: generate_xrpl_address(),
                        currency: coreum_originated_token.xrpl_currency.clone(),
                        amount: amount_to_send_back.clone(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // If we send the token with a different currency (one that is not the one in the registered token list) it should fail
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: bridge_xrpl_address.clone(),
                        currency: "invalid_currency".to_string(),
                        amount: amount_to_send_back.clone(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // Sending under the minimum should fail (minimum - 1)
        let transfer_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: bridge_xrpl_address.clone(),
                        currency: coreum_originated_token.xrpl_currency.clone(),
                        amount: amount_to_send_back.checked_sub(Uint128::one()).unwrap(),
                        recipient: Addr::unchecked(sender.address()),
                    },
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(transfer_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Sending the right evidence should move tokens from the contract to the sender's account
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: bridge_xrpl_address.clone(),
                    currency: coreum_originated_token.xrpl_currency.clone(),
                    amount: amount_to_send_back.clone(),
                    recipient: Addr::unchecked(sender.address()),
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        // Check balance of sender and contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send) // initial amount
                .unwrap()
                .checked_sub(Uint128::new(9999999999)) // Amount lost during first truncation that was rejected
                .unwrap()
                .checked_add(Uint128::new(10000000000)) // Amount that we sent back after conversion (1e10), the minimum
                .unwrap()
                .to_string()
        );

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            amount_to_send
                .checked_add(Uint128::new(9999999999)) // Amount that was kept during truncation of rejected operation
                .unwrap()
                .checked_sub(Uint128::new(10000000000)) // Amount sent from XRPL to the user
                .unwrap()
                .to_string()
        );
    }

    #[test]
    fn send_from_coreum_to_xrpl() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let sender = accounts.get(1).unwrap();
        let relayer_account = accounts.get(2).unwrap();
        let relayer = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);
        let xrpl_base_fee = 10;
        let multisig_address = generate_xrpl_address();

        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            10,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            multisig_address.clone(),
            xrpl_base_fee,
        );

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let denom_xrp = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == XRP_ISSUER && t.currency == XRP_CURRENCY)
            .unwrap()
            .coreum_denom
            .clone();

        // Add enough tickets for all our test operations

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(11),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(tx_hash.clone()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..12).collect()),
                    }),
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // If we query processed Txes with this tx_hash it should return true
        let query_processed_tx = wasm
            .query::<QueryMsg, bool>(
                &contract_addr,
                &QueryMsg::ProcessedTx {
                    hash: tx_hash.to_uppercase(),
                },
            )
            .unwrap();

        assert_eq!(query_processed_tx, true);

        // If we query something that is not processed it should return false
        let query_processed_tx = wasm
            .query::<QueryMsg, bool>(
                &contract_addr,
                &QueryMsg::ProcessedTx {
                    hash: generate_hash(),
                },
            )
            .unwrap();

        assert_eq!(query_processed_tx, false);

        // *** Test sending XRP back to XRPL, which is already enabled so we can bridge it directly ***

        let amount_to_send_xrp = Uint128::new(50000);
        let amount_to_send_back = Uint128::new(10000);
        let final_balance_xrp = amount_to_send_xrp.checked_sub(amount_to_send_back).unwrap();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    amount: amount_to_send_xrp.clone(),
                    recipient: Addr::unchecked(sender.address()),
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        // Check that balance is in the sender's account
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrp.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount_to_send_xrp.to_string());

        let xrpl_receiver_address = generate_xrpl_address();
        // Trying to send XRP back with a deliver_amount should fail
        let deliver_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(Uint128::one()),
                },
                &coins(amount_to_send_back.u128(), denom_xrp.clone()),
                sender,
            )
            .unwrap_err();

        assert!(deliver_error.to_string().contains(
            ContractError::DeliverAmountIsProhibited {}
                .to_string()
                .as_str()
        ));

        // Send the XRP back to XRPL successfully
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send_back.u128(), denom_xrp.clone()),
            sender,
        )
        .unwrap();

        // Check that operation is in the queue
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(1),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    amount: amount_to_send_back,
                    max_amount: None,
                    sender: Addr::unchecked(sender.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee,
            }
        );

        // If we try to send tokens from Coreum to XRPL using the multisig address as recipient, it should fail.
        let bridge_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: multisig_address,
                    deliver_amount: None,
                },
                &coins(1, denom_xrp.clone()),
                sender,
            )
            .unwrap_err();

        assert!(bridge_error
            .to_string()
            .contains(ContractError::ProhibitedRecipient {}.to_string().as_str()));

        // If we try to send tokens from Coreum to XRPL using a prohibited recipient address, it should fail.
        let bridge_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: INITIAL_PROHIBITED_XRPL_RECIPIENTS[0].to_string(),
                    deliver_amount: None,
                },
                &coins(1, denom_xrp.clone()),
                sender,
            )
            .unwrap_err();

        assert!(bridge_error
            .to_string()
            .contains(ContractError::ProhibitedRecipient {}.to_string().as_str()));

        // Sending a CoreumToXRPLTransfer evidence with account sequence should fail.
        let invalid_evidence = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(generate_hash()),
                        account_sequence: Some(1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer_account,
            )
            .unwrap_err();

        assert!(invalid_evidence.to_string().contains(
            ContractError::InvalidTransactionResultEvidence {}
                .to_string()
                .as_str()
        ));

        // Send successful evidence to remove from queue (tokens should be released on XRPL to the receiver)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(1),
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 0);

        // Since transaction result was Accepted, the tokens must have been burnt
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrp.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, final_balance_xrp.to_string());

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom_xrp.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, Uint128::zero().to_string());

        // Now we will try to send back again but this time reject it, thus balance must be sent back to the sender.

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send_back.u128(), denom_xrp.clone()),
            sender,
        )
        .unwrap();

        // Transaction was rejected
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(2),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Since transaction result was Rejected, the tokens must have been sent to pending refunds

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom_xrp.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, amount_to_send_back.to_string());

        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // We verify that these tokens are refundable
        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(amount_to_send_back.u128(), denom_xrp.clone())
        );

        // *** Test sending an XRPL originated token back to XRPL ***

        let test_token = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "TST".to_string(),
            sending_precision: 15,
            max_holding_amount: Uint128::new(50000000000000000000), // 5e20
            bridging_fee: Uint128::zero(),
        };

        // First we need to register and activate it
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token.issuer.clone(),
                currency: test_token.currency.clone(),
                sending_precision: test_token.sending_precision,
                max_holding_amount: test_token.max_holding_amount,
                bridging_fee: test_token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        let amount_to_send = Uint128::new(10000000000000000000); // 1e20
        let final_balance = amount_to_send.checked_sub(amount_to_send_back).unwrap();
        // Bridge some tokens to the sender address
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token.issuer.to_string(),
                    currency: test_token.currency.to_string(),
                    amount: amount_to_send.clone(),
                    recipient: Addr::unchecked(sender.address()),
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let xrpl_originated_token = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token.issuer && t.currency == test_token.currency)
            .unwrap();
        let denom_xrpl_origin_token = xrpl_originated_token.coreum_denom.clone();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, amount_to_send.to_string());

        // If we send more than one token in the funds we should get an error
        let invalid_funds_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: None,
                },
                &vec![
                    coin(1, FEE_DENOM),
                    coin(amount_to_send_back.u128(), denom_xrpl_origin_token.clone()),
                ],
                sender,
            )
            .unwrap_err();

        assert!(invalid_funds_error.to_string().contains(
            ContractError::Payment(cw_utils::PaymentError::MultipleDenoms {})
                .to_string()
                .as_str()
        ));

        // If we send to an invalid XRPL address we should get an error
        let invalid_address_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: "invalid_address".to_string(),
                    deliver_amount: None,
                },
                &coins(amount_to_send_back.u128(), denom_xrpl_origin_token.clone()),
                sender,
            )
            .unwrap_err();

        assert!(invalid_address_error.to_string().contains(
            ContractError::InvalidXRPLAddress {
                address: "invalid_address".to_string()
            }
            .to_string()
            .as_str()
        ));

        // We will send a successful transfer to XRPL considering the token has no transfer rate

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send_back.u128(), denom_xrpl_origin_token.clone()),
            sender,
        )
        .unwrap();

        // Check that the operation was added to the queue

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(4),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: xrpl_originated_token.issuer.clone(),
                    currency: xrpl_originated_token.currency.clone(),
                    amount: amount_to_send_back,
                    max_amount: Some(amount_to_send_back),
                    sender: Addr::unchecked(sender.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee
            }
        );

        // Send successful should burn the tokens
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(4),
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 0);

        // Tokens should have been burnt since transaction was accepted
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, final_balance.to_string());

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, Uint128::zero().to_string());

        // Now we will try to send back again but this time reject it, thus balance must be sent back to the sender
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send_back.u128(), denom_xrpl_origin_token.clone()),
            sender,
        )
        .unwrap();

        // Send rejected should store tokens minus truncated amount in refundable amount for the sender
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(5),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrp.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            final_balance_xrp
                .checked_sub(amount_to_send_back)
                .unwrap()
                .to_string()
        );

        // Let's check the pending refunds for the sender and also check that pagination works correctly.
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        // There was one pending refund from previous test, we are going to claim both
        assert_eq!(query_pending_refunds.pending_refunds.len(), 2);

        // Test with limit 1 and starting after first one
        let query_pending_refunds_with_limit = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: Some(1),
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds_with_limit.pending_refunds.len(), 1);

        // Test with limit 1 and starting from first key
        let query_pending_refunds_with_limit_and_start_after_key = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: query_pending_refunds_with_limit.last_key,
                    limit: Some(1),
                },
            )
            .unwrap();

        assert_eq!(
            query_pending_refunds_with_limit_and_start_after_key
                .pending_refunds
                .len(),
            1
        );
        assert_eq!(
            query_pending_refunds_with_limit_and_start_after_key.pending_refunds[0],
            query_pending_refunds.pending_refunds[1]
        );

        // Let's claim all pending refunds and check that they are gone from the contract and in the senders address
        for refund in query_pending_refunds.pending_refunds.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRefund {
                    pending_refund_id: refund.id.clone(),
                },
                &[],
                &sender,
            )
            .unwrap();
        }

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrp.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, final_balance_xrp.to_string());

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom_xrp.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, Uint128::zero().to_string());

        // Let's test sending a token with optional amount

        let max_amount = Uint128::new(9999999999999999);
        let deliver_amount = Some(Uint128::new(6000));

        // Store balance first so we can check it later
        let request_initial_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();

        // If we send amount that is higher than max amount, it should fail
        let max_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(max_amount.checked_add(Uint128::one()).unwrap()),
                },
                &coins(max_amount.u128(), denom_xrpl_origin_token.clone()),
                sender,
            )
            .unwrap_err();

        assert!(max_amount_error
            .to_string()
            .contains(ContractError::InvalidDeliverAmount {}.to_string().as_str()));

        // If we send a deliver amount that is an invalid XRPL amount, it should fail
        let invalid_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(Uint128::new(99999999999999999)),
                },
                &coins(1000000000000000000, denom_xrpl_origin_token.clone()),
                sender,
            )
            .unwrap_err();

        assert!(invalid_amount_error
            .to_string()
            .contains(ContractError::InvalidXRPLAmount {}.to_string().as_str()));

        // If we send an amount that is an invalid XRPL amount, it should fail
        let invalid_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(Uint128::new(10000000000000000)),
                },
                &coins(10000000000000001, denom_xrpl_origin_token.clone()),
                sender,
            )
            .unwrap_err();

        assert!(invalid_amount_error
            .to_string()
            .contains(ContractError::InvalidXRPLAmount {}.to_string().as_str()));

        // Send it correctly
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount,
            },
            &coins(max_amount.u128(), denom_xrpl_origin_token.clone()),
            sender,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(6),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: xrpl_originated_token.issuer.clone(),
                    currency: xrpl_originated_token.currency.clone(),
                    amount: deliver_amount.unwrap(),
                    max_amount: Some(max_amount),
                    sender: Addr::unchecked(sender.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee
            }
        );

        // If we reject the operation, the refund should be stored for the amount of funds that were sent
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(6),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Check balances and pending refunds are all correct
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            request_initial_balance
                .balance
                .parse::<u128>()
                .unwrap()
                .checked_sub(max_amount.u128())
                .unwrap()
                .to_string()
        );

        // Let's check the pending refunds for the sender and also check that pagination works correctly.
        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(max_amount.u128(), denom_xrpl_origin_token.clone())
        );

        // Claim it back

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap();

        // Check balance again
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom_xrpl_origin_token.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, request_initial_balance.balance);

        // *** Test sending Coreum originated tokens to XRPL

        // Let's issue a token to the sender and register it.
        let asset_ft = AssetFT::new(&app);
        let symbol = "TEST".to_string();
        let subunit = "utest".to_string();
        let initial_amount = Uint128::new(1000000000);
        let decimals = 6;
        asset_ft
            .issue(
                MsgIssue {
                    issuer: sender.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &sender,
            )
            .unwrap();

        let denom = format!("{}-{}", subunit, sender.address()).to_lowercase();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: denom.clone(),
                decimals,
                sending_precision: 5,
                max_holding_amount: Uint128::new(10000000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let amount_to_send = Uint128::new(1000001); // 1000001 -> truncate -> 1e6 -> decimal conversion -> 1e15

        // Bridge the token to the xrpl receiver address two times and check pending operations
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(amount_to_send.u128(), denom.clone()),
            &sender,
        )
        .unwrap();

        let multisig_address = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap()
            .bridge_xrpl_address;

        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let coreum_originated_token = query_coreum_tokens
            .tokens
            .iter()
            .find(|t| t.denom == denom)
            .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 2);
        let amount = amount_to_send
            .checked_sub(Uint128::one()) //Truncated amount
            .unwrap()
            .checked_mul(Uint128::new(10u128.pow(9))) // XRPL Decimals - Coreum Decimals -> (15 - 6) = 9
            .unwrap();
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(7),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: multisig_address.clone(),
                    currency: coreum_originated_token.xrpl_currency.clone(),
                    amount: amount.clone(),
                    max_amount: Some(amount.clone()),
                    sender: Addr::unchecked(sender.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee
            }
        );

        assert_eq!(
            query_pending_operations.operations[1],
            Operation {
                id: query_pending_operations.operations[1].id.clone(),
                version: 1,
                ticket_sequence: Some(8),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: multisig_address,
                    currency: coreum_originated_token.xrpl_currency.clone(),
                    amount: amount.clone(),
                    max_amount: Some(amount.clone()),
                    sender: Addr::unchecked(sender.address()),
                    recipient: xrpl_receiver_address,
                },
                xrpl_base_fee
            }
        );

        // If we reject both operations, the tokens should be kept in pending refunds with different ids for the sender to claim (except truncated amount)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(7),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(8),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        // Refundable amount (amount to send x 2 - truncated amount x 2) won't be sent back until claimed individually
        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(amount_to_send)
                .unwrap()
                .checked_sub(amount_to_send)
                .unwrap()
                .to_string()
        );

        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(sender.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds.len(), 2);

        // Claiming pending refund should work for both operations
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[1].id.clone(),
            },
            &[],
            &sender,
        )
        .unwrap();

        // Check that balance was correctly sent back
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(
            request_balance.balance,
            initial_amount
                .checked_sub(Uint128::new(2))
                .unwrap()
                .to_string()
        );

        // Truncated amount will stay in contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();
        assert_eq!(request_balance.balance, Uint128::new(2).to_string());

        // Let's query all processed transactions
        let query_processed_txs = wasm
            .query::<QueryMsg, ProcessedTxsResponse>(
                &contract_addr,
                &QueryMsg::ProcessedTxs {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_processed_txs.processed_txs.len(), 11);

        // Let's query with pagination
        let query_processed_txs = wasm
            .query::<QueryMsg, ProcessedTxsResponse>(
                &contract_addr,
                &QueryMsg::ProcessedTxs {
                    start_after_key: None,
                    limit: Some(4),
                },
            )
            .unwrap();

        assert_eq!(query_processed_txs.processed_txs.len(), 4);

        let query_processed_txs = wasm
            .query::<QueryMsg, ProcessedTxsResponse>(
                &contract_addr,
                &QueryMsg::ProcessedTxs {
                    start_after_key: query_processed_txs.last_key,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_processed_txs.processed_txs.len(), 7);
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
            generate_xrpl_address(),
            10,
        );

        // *** Test with XRPL originated tokens ***

        let test_token1 = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "TT1".to_string(),
            sending_precision: -2,
            max_holding_amount: Uint128::new(200000000000000000),
            bridging_fee: Uint128::zero(),
        };
        let test_token2 = XRPLToken {
            issuer: generate_xrpl_address().to_string(),
            currency: "TT2".to_string(),
            sending_precision: 13,
            max_holding_amount: Uint128::new(499),
            bridging_fee: Uint128::zero(),
        };

        let test_token3 = XRPLToken {
            issuer: generate_xrpl_address().to_string(),
            currency: "TT3".to_string(),
            sending_precision: 0,
            max_holding_amount: Uint128::new(5000000000000000),
            bridging_fee: Uint128::zero(),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..9).collect()),
                    }),
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
                max_holding_amount: test_token1.max_holding_amount.clone(),
                bridging_fee: test_token1.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
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
                    // Sending more than 199999999999999999 will truncate to 100000000000000000 and send it to the user and keep the remainder in the contract as fees to collect.
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

        // Sending anything again should not work because we already sent the maximum amount possible including the fees in the contract.
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token1.issuer.clone(),
                        currency: test_token1.currency.clone(),
                        amount: Uint128::new(100000000000000000),
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
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        // Fees collected
        assert_eq!(request_balance.balance, "99999999999999999".to_string());

        // Test positive sending precisions

        // Register token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token2.issuer.clone(),
                currency: test_token2.currency.clone(),
                sending_precision: test_token2.sending_precision.clone(),
                max_holding_amount: test_token2.max_holding_amount.clone(),
                bridging_fee: test_token2.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
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
                    start_after_key: None,
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
                    // Sending 299 should truncate the amount to 200 and keep the 99 in the contract as fees to collect
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

        // Sending 200 should work because we will reach exactly the maximum bridged amount.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: test_token2.issuer.clone(),
                    currency: test_token2.currency.clone(),
                    amount: Uint128::new(200),
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

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "99".to_string());

        // Sending anything again should fail because we passed the maximum bridged amount
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: generate_hash(),
                        issuer: test_token2.issuer.clone(),
                        currency: test_token2.currency.clone(),
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
                max_holding_amount: test_token3.max_holding_amount.clone(),
                bridging_fee: test_token3.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // Activate the token
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
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
                    operation_result: None,
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
                    start_after_key: None,
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
                    // Sending 1111111111111111 should truncate the amount to 1000000000000000 and keep 111111111111111 as fees to collect
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
                    // Sending 3111111111111111 should truncate the amount to 3000000000000000 and keep another 111111111111111 as fees to collect
                    amount: Uint128::new(3111111111111111),
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

        assert_eq!(request_balance.balance, "4000000000000000".to_string());

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "222222222222222".to_string());

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
                    amount: Uint128::one(),
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
                        amount: Uint128::one(),
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

        // *** Test with Coreum originated tokens ***

        // Let's issue a few assets to the sender and registering them with different precisions and max sending amounts.
        let asset_ft = AssetFT::new(&app);
        for i in 1..=3 {
            let symbol = "TEST".to_string() + &i.to_string();
            let subunit = "utest".to_string() + &i.to_string();
            asset_ft
                .issue(
                    MsgIssue {
                        issuer: signer.address(),
                        symbol,
                        subunit,
                        precision: 6,
                        initial_amount: "100000000000000".to_string(),
                        description: "description".to_string(),
                        features: vec![MINTING as i32],
                        burn_rate: "0".to_string(),
                        send_commission_rate: "0".to_string(),
                        uri: "uri".to_string(),
                        uri_hash: "uri_hash".to_string(),
                    },
                    &signer,
                )
                .unwrap();
        }

        let denom1 = format!("{}-{}", "utest1", signer.address()).to_lowercase();
        let denom2 = format!("{}-{}", "utest2", signer.address()).to_lowercase();
        let denom3 = format!("{}-{}", "utest3", signer.address()).to_lowercase();

        let test_tokens = vec![
            CoreumToken {
                denom: denom1.clone(),
                decimals: 6,
                sending_precision: 6,
                max_holding_amount: Uint128::new(3),
                bridging_fee: Uint128::zero(),
            },
            CoreumToken {
                denom: denom2.clone(),
                decimals: 6,
                sending_precision: 0,
                max_holding_amount: Uint128::new(3990000),
                bridging_fee: Uint128::zero(),
            },
            CoreumToken {
                denom: denom3.clone(),
                decimals: 6,
                sending_precision: -6,
                max_holding_amount: Uint128::new(2000000000000),
                bridging_fee: Uint128::zero(),
            },
        ];

        // Register the tokens

        for token in test_tokens.clone() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: token.denom,
                    decimals: token.decimals,
                    sending_precision: token.sending_precision,
                    max_holding_amount: token.max_holding_amount,
                    bridging_fee: token.bridging_fee,
                },
                &vec![],
                &signer,
            )
            .unwrap();
        }

        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_coreum_tokens.tokens.len(), 3);
        assert_eq!(query_coreum_tokens.tokens[0].denom, test_tokens[0].denom);
        assert_eq!(query_coreum_tokens.tokens[1].denom, test_tokens[1].denom);
        assert_eq!(query_coreum_tokens.tokens[2].denom, test_tokens[2].denom);

        // Test sending token 1 with high precision

        // Sending 2 would work as it hasn't reached the maximum holding amount yet
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(2, denom1.clone()),
            &signer,
        )
        .unwrap();

        // Sending 1 more will hit max amount but will not fail
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(1, denom1.clone()),
            &signer,
        )
        .unwrap();

        // Trying to send 1 again would fail because we go over max bridge amount
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1, denom1.clone()),
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
                account: contract_addr.clone(),
                denom: denom1.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "3".to_string());

        // Test sending token 2 with medium precision

        // Sending under sending precision would return error because it will be truncated to 0.
        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(100000, denom2.clone()),
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Sending 3990000 would work as it is the maximum bridgable amount
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(3990000, denom2.clone()),
            &signer,
        )
        .unwrap();

        // Sending 100000 will fail because truncating will truncate to 0.
        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(100000, denom2.clone()),
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Trying to send 1000000 would fail because we go over max bridge amount
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1000000, denom2.clone()),
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
                account: contract_addr.clone(),
                denom: denom2.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "3990000".to_string());

        // Test sending token 3 with low precision

        // Sending 2000000000000 would work as it is the maximum bridgable amount
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(2000000000000, denom3.clone()),
            &signer,
        )
        .unwrap();

        // Sending 200000000000 (1 less zero) will fail because truncating will truncate to 0.
        let precision_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(200000000000, denom3.clone()),
                &signer,
            )
            .unwrap_err();

        assert!(precision_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Trying to send 1000000000000 would fail because we go over max bridge amount
        let maximum_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1000000000000, denom3.clone()),
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
                account: contract_addr.clone(),
                denom: denom3.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "2000000000000".to_string());
    }

    #[test]
    fn bridge_fee_collection_and_claiming() {
        let app = CoreumTestApp::new();
        let accounts_number = 5;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let receiver = accounts.get((accounts_number - 2) as usize).unwrap();
        let xrpl_addresses: Vec<String> = (0..3).map(|_| generate_xrpl_address()).collect();
        let xrpl_pub_keys: Vec<String> = (0..3).map(|_| generate_xrpl_pub_key()).collect();

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
        let xrpl_base_fee = 10;

        let bridge_xrpl_address = generate_xrpl_address();
        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![
                relayers[0].clone(),
                relayers[1].clone(),
                relayers[2].clone(),
            ],
            3,
            14,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            xrpl_base_fee,
        );

        // Recover enough tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(15),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: Some((1..16).collect()),
                        }),
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // We are going to issue 2 tokens, one XRPL originated and one Coreum originated, with different fees.
        let test_token_xrpl = XRPLToken {
            issuer: generate_xrpl_address(), // Valid issuer
            currency: "USD".to_string(),     // Valid standard currency code
            sending_precision: 10,
            max_holding_amount: Uint128::new(5000000000000000), // 5e15
            bridging_fee: Uint128::new(50000),                  // 5e4
        };

        let symbol = "TEST".to_string();
        let subunit = "utest".to_string();
        let decimals = 6;
        let initial_amount = Uint128::new(100000000);
        asset_ft
            .issue(
                MsgIssue {
                    issuer: receiver.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &receiver,
            )
            .unwrap();

        let coreum_token_denom = format!("{}-{}", subunit, receiver.address()).to_lowercase();

        let test_token_coreum = CoreumToken {
            denom: coreum_token_denom.clone(),
            decimals,
            sending_precision: 4,
            max_holding_amount: Uint128::new(10000000000), // 1e10
            bridging_fee: Uint128::new(300000),            // 3e5
        };

        // Register XRPL originated token and confirm trust set
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: test_token_xrpl.issuer.clone(),
                currency: test_token_xrpl.currency.clone(),
                sending_precision: test_token_xrpl.sending_precision,
                max_holding_amount: test_token_xrpl.max_holding_amount,
                bridging_fee: test_token_xrpl.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: None,
                        ticket_sequence: Some(1),
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let xrpl_token = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == test_token_xrpl.issuer && t.currency == test_token_xrpl.currency)
            .unwrap();

        // Register Coreum originated token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: test_token_coreum.denom,
                decimals: test_token_coreum.decimals,
                sending_precision: test_token_coreum.sending_precision,
                max_holding_amount: test_token_coreum.max_holding_amount,
                bridging_fee: test_token_coreum.bridging_fee,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let coreum_token = query_coreum_tokens
            .tokens
            .iter()
            .find(|t| t.denom == coreum_token_denom)
            .unwrap();

        // Let's bridge some tokens from XRPL to Coreum multiple times and verify that the fees are collected correctly in each step
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: test_token_xrpl.issuer.clone(),
                        currency: test_token_xrpl.currency.clone(),
                        amount: Uint128::new(1000000000050000), // 1e15 + 5e4 --> This should take the bridging fee (5e4) and truncate nothing
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: xrpl_token.coreum_denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "1000000000000000".to_string());

        // If we query fees for any random address that has no fees collected, it should return an empty array
        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked("any_address"),
                },
            )
            .unwrap();

        assert_eq!(query_fees_collected.fees_collected, vec![]);

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // 50000 / 3 = 16666.67 ---> Which means each relayer will have 16666 to claim and 2 tokens will stay in the fee remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(16666, xrpl_token.coreum_denom.clone())]
        );

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: test_token_xrpl.issuer.clone(),
                        currency: test_token_xrpl.currency.clone(),
                        amount: Uint128::new(1000000000040000), // 1e15 + 4e4 --> This should take the bridging fee -> 1999999999990000 and truncate -> 1999999999900000
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: xrpl_token.coreum_denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "1999999999900000".to_string());

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 140000 (+2 that were in the remainder) / 3 -> 140002 / 3 = 46667 and 1 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(63333, xrpl_token.coreum_denom.clone())] // 16666 from before + 46667
        );

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: test_token_xrpl.issuer.clone(),
                        currency: test_token_xrpl.currency.clone(),
                        amount: Uint128::new(1000000000000000), // 1e15 --> This should charge bridging fee -> 1999999999950000 and truncate -> 1999999999900000
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: xrpl_token.coreum_denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "2999999999800000".to_string());

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 100000 (+1 from remainder) / 3 -> 100001 / 3 = 33333 and 2 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(96666, xrpl_token.coreum_denom.clone())] // 63333 from before + 33333
        );

        // Check that contract holds those tokens.
        let query_contract_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: xrpl_token.coreum_denom.clone(),
            })
            .unwrap();
        assert_eq!(query_contract_balance.balance, "290000".to_string()); // 96666 * 3 + 2 in the remainder

        // Let's try to bridge some tokens back from Coreum to XRPL and verify that the fees are also collected correctly
        let xrpl_receiver_address = generate_xrpl_address();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(1000000000020000, xrpl_token.coreum_denom.clone()), // This should charge the bridging fee -> 999999999970000 and then truncate the rest -> 999999999900000
            &receiver,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(2),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: test_token_xrpl.issuer.clone(),
                    currency: test_token_xrpl.currency.clone(),
                    amount: Uint128::new(999999999900000),
                    max_amount: Some(Uint128::new(999999999900000)),
                    sender: Addr::unchecked(receiver.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee,
            }
        );

        // Confirm operation to clear tokens from contract
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: query_pending_operations.operations[0].account_sequence,
                        ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 120000 (+2 from remainder) / 3 -> 120002 / 3 = 40000 and 2 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(136666, xrpl_token.coreum_denom.clone())] // 96666 from before + 40000
        );

        // Let's bridge some tokens again but this time with the optional amount, to check that bridge fees are collected correctly and
        // when rejected, full amount without bridge fees is available to be claimed back by user.
        let deliver_amount = Some(Uint128::new(700000000020000));

        // If we send an amount, that after truncation and bridge fees is higher than max amount, it should fail
        let max_amount_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: Some(Uint128::new(1000000000010000)),
                },
                &coins(1000000000020000, xrpl_token.coreum_denom.clone()), // After fees and truncation -> 1000000000000000 > 999999999900000
                &receiver,
            )
            .unwrap_err();

        assert!(max_amount_error
            .to_string()
            .contains(ContractError::InvalidDeliverAmount {}.to_string().as_str()));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount, // This will be truncated to 700000000000000
            },
            &coins(1000000000020000, xrpl_token.coreum_denom.clone()), // This should charge the bridging fee -> 999999999970000 and then truncate the rest -> 999999999900000
            &receiver,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(3),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: test_token_xrpl.issuer.clone(),
                    currency: test_token_xrpl.currency.clone(),
                    amount: Uint128::new(700000000000000),
                    max_amount: Some(Uint128::new(999999999900000)),
                    sender: Addr::unchecked(receiver.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee
            }
        );

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 120000 (+2 from remainder) / 3 -> 120002 / 3 = 40000 and 2 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(176666, xrpl_token.coreum_denom.clone())] // 136666 from before + 40000
        );

        // If we reject the operation, 999999999900000 (max_amount after bridge fees and truncation) should be able to be claimed back by the user
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: query_pending_operations.operations[0].account_sequence,
                        ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                        transaction_result: TransactionResult::Rejected,
                        operation_result: None,
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(receiver.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(999999999900000, xrpl_token.coreum_denom.clone())
        );

        // Let's claim it back
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ClaimRefund {
                pending_refund_id: query_pending_refunds.pending_refunds[0].id.clone(),
            },
            &[],
            &receiver,
        )
        .unwrap();

        // Now let's bridge tokens from Coreum to XRPL and verify that the fees are collected correctly in each step and accumulated with the previous ones

        // Trying to send less than the bridging fees should fail
        let bridging_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: xrpl_receiver_address.clone(),
                    deliver_amount: None,
                },
                &coins(100, coreum_token_denom.clone()),
                &receiver,
            )
            .unwrap_err();

        assert!(bridging_error.to_string().contains(
            ContractError::CannotCoverBridgingFees {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(600010, coreum_token_denom.clone()), // This should charge briding fee -> 300010 and then truncate the rest -> 300000
            &receiver,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(4),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: bridge_xrpl_address.clone(),
                    currency: coreum_token.xrpl_currency.clone(),
                    amount: Uint128::new(300000000000000),
                    max_amount: Some(Uint128::new(300000000000000)),
                    sender: Addr::unchecked(receiver.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee
            }
        );

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 300010 / 3 -> 100003 and 1 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![
                coin(176666, xrpl_token.coreum_denom.clone()),
                coin(100003, coreum_token_denom.clone())
            ]
        );

        // Confirm operation
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: query_pending_operations.operations[0].account_sequence,
                        ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(900000, coreum_token_denom.clone()), // This charge the entire bridging fee (300000) and truncate nothing
            &receiver,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(5),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::CoreumToXRPLTransfer {
                    issuer: bridge_xrpl_address.clone(),
                    currency: coreum_token.xrpl_currency.clone(),
                    amount: Uint128::new(600000000000000),
                    max_amount: Some(Uint128::new(600000000000000)),
                    sender: Addr::unchecked(receiver.address()),
                    recipient: xrpl_receiver_address.clone(),
                },
                xrpl_base_fee,
            }
        );

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer is getting 300000 (+1 from remainder) / 3 -> 100000 and 1 token will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![
                coin(176666, xrpl_token.coreum_denom.clone()),
                coin(200003, coreum_token_denom.clone()) // 100003 + 100000
            ]
        );

        // Confirm operation
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: query_pending_operations.operations[0].account_sequence,
                        ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        // Let's try to send the Coreum originated token in the opposite direction (from XRPL to Coreum) and see that fees are also accumulated correctly.
        let previous_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: coreum_token_denom.clone(),
            })
            .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: bridge_xrpl_address.clone(),
                        currency: coreum_token.xrpl_currency.clone(),
                        amount: Uint128::new(650010000000000), // 650010000000000 will convert to 650010, which after charging bridging fees (300000) and truncating (10) will send 350000 to the receiver
                        recipient: Addr::unchecked(receiver.address()),
                    },
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let new_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: receiver.address(),
                denom: coreum_token_denom.clone(),
            })
            .unwrap();

        assert_eq!(
            new_balance.balance.parse::<u128>().unwrap(),
            previous_balance
                .balance
                .parse::<u128>()
                .unwrap()
                .checked_add(350000)
                .unwrap()
        );

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // Each relayer will be getting 300010 (+1 from the remainder) / 3 -> 300011 / 3 = 100003 and 2 tokens will stay in the remainders for next collection
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![
                coin(176666, xrpl_token.coreum_denom.clone()),
                coin(300006, coreum_token_denom.clone()) // 200003 from before + 100003
            ]
        );

        // Let's test the claiming

        // If we claim more than available, it should fail
        let claim_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![
                        coin(176666, xrpl_token.coreum_denom.clone()),
                        coin(300007, coreum_token_denom.clone()), // +1
                    ],
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(claim_error.to_string().contains(
            ContractError::NotEnoughFeesToClaim {
                denom: coreum_token_denom.clone(),
                amount: Uint128::new(300007)
            }
            .to_string()
            .as_str()
        ));

        // If we separate token claim into two coins but ask for too much it should also fail
        let claim_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![
                        coin(176666, xrpl_token.coreum_denom.clone()),
                        coin(300006, coreum_token_denom.clone()),
                        coin(1, coreum_token_denom.clone()), // Extra token claim that is too much
                    ],
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(claim_error.to_string().contains(
            ContractError::NotEnoughFeesToClaim {
                denom: coreum_token_denom.clone(),
                amount: Uint128::new(1)
            }
            .to_string()
            .as_str()
        ));

        // If we claim everything except 1 token, it should work
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![
                        coin(176666, xrpl_token.coreum_denom.clone()),
                        coin(300005, coreum_token_denom.clone()),
                    ],
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        let query_fees_collected = wasm
            .query::<QueryMsg, FeesCollectedResponse>(
                &contract_addr,
                &QueryMsg::FeesCollected {
                    relayer_address: Addr::unchecked(relayer_accounts[0].address()),
                },
            )
            .unwrap();

        // There should be only 1 token left in the remainders
        assert_eq!(
            query_fees_collected.fees_collected,
            vec![coin(1, coreum_token_denom.clone())]
        );

        // If we try to claim a token that is not in the claimable array, it should fail
        let claim_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![coin(1, xrpl_token.coreum_denom.clone())],
                },
                &[],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(claim_error.to_string().contains(
            ContractError::NotEnoughFeesToClaim {
                denom: xrpl_token.coreum_denom.clone(),
                amount: Uint128::new(1)
            }
            .to_string()
            .as_str()
        ));

        // Claim the token that is left to claim
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![coin(1, coreum_token_denom.clone())],
                },
                &[],
                relayer,
            )
            .unwrap();
        }

        // Let's check the balances of the relayers
        for relayer in relayer_accounts.iter() {
            let request_balance_token1 = asset_ft
                .query_balance(&QueryBalanceRequest {
                    account: relayer.address(),
                    denom: xrpl_token.coreum_denom.clone(),
                })
                .unwrap();
            let request_balance_token2 = asset_ft
                .query_balance(&QueryBalanceRequest {
                    account: relayer.address(),
                    denom: coreum_token_denom.clone(),
                })
                .unwrap();

            assert_eq!(request_balance_token1.balance, "176666".to_string()); // 530000 / 3 = 183333
            assert_eq!(request_balance_token2.balance, "300006".to_string()); // 900020 / 3 = 300006
        }

        // We check that everything has been claimed
        for relayer in relayer_accounts.iter() {
            let query_fees_collected = wasm
                .query::<QueryMsg, FeesCollectedResponse>(
                    &contract_addr,
                    &QueryMsg::FeesCollected {
                        relayer_address: Addr::unchecked(relayer.address()),
                    },
                )
                .unwrap();

            assert_eq!(query_fees_collected.fees_collected, vec![]);
        }

        // Check that final balance in the contract matches with those fees
        let query_contract_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: xrpl_token.coreum_denom.clone(),
            })
            .unwrap();
        assert_eq!(query_contract_balance.balance, "2".to_string()); // What is stored in the remainder

        let query_contract_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: coreum_token_denom.clone(),
            })
            .unwrap();

        // Amount that the user can still bridge back (he has on XRPL) from the token he has sent
        // Sent: 300000 + 600000 (after applying fees and truncating)
        // Sent back: 650010
        // Result: 300000 + 600000 - 650010 = 249990
        // + 2 tokens that have not been claimed yet because the relayers can't claim them = 249992
        assert_eq!(query_contract_balance.balance, "249992".to_string());
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
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayers[0].clone(), relayers[1].clone()],
            2,
            4,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            xrpl_base_fee,
        );

        // Querying current pending operations and available tickets should return empty results.
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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

        assert!(query_pending_operations.operations.is_empty());
        assert!(query_available_tickets.tickets.is_empty());

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

        // Check that we can recover tickets and provide signatures for this operation with the bridge halted
        wasm.execute::<ExecuteMsg>(&contract_addr, &ExecuteMsg::HaltBridge {}, &vec![], &signer)
            .unwrap();

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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(
            query_pending_operations.operations,
            [Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: None,
                account_sequence: Some(account_sequence),
                signatures: vec![], // No signatures yet
                operation_type: OperationType::AllocateTickets { number: 5 },
                xrpl_base_fee
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
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: None,
                        }),
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

        // Providing an invalid signature for the operation should error
        let signature_error = wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                operation_version: 1,
                signature: "3045022100DFA01DA5D6C9877F9DAA59A06032247F3D7ED6444EAD5C90A3AC33CCB7F19B3F02204D8D50E4D085BB1BC9DFB8281B8F35BDAEB7C74AE4B825F8CAE1217CFBDF4EA13045022100DFA01DA5D6C9877F9DAA59A06032247F3D7ED6444EAD5C90A3AC33CCB7F19B3F02204D8D50E4D085BB1BC9DFB8281B8F35BDAEB7C74AE4B825F8CAE1217CFBDF4EA1".to_string(),
            },
            &vec![],
            relayer_accounts[0],
        )
        .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::InvalidSignatureLength {}
                .to_string()
                .as_str()
        ));

        // Provide signatures for the operation for each relayer
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                operation_version: 1,
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
                    operation_version: 1,
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
                    operation_version: 1,
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

        // Provide a signature for an operation with a different version should fail
        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveSignature {
                    operation_id: account_sequence,
                    operation_version: 2,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(signature_error.to_string().contains(
            ContractError::OperationVersionMismatch {}
                .to_string()
                .as_str()
        ));

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: account_sequence,
                operation_version: 1,
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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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
                    operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
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
                    operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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

        // Resume bridge
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ResumeBridge {},
            &vec![],
            &signer,
        )
        .unwrap();

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
                operation_version: 1,
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
                operation_version: 1,
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
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: Some(tickets.clone()),
                        }),
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
                    operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
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
                    operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    }),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some(tickets.clone()),
                    }),
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
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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
    fn xrpl_token_registration_recovery() {
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

        let token_issuer = generate_xrpl_address();
        let token_currency = "BTC".to_string();
        let token = XRPLToken {
            issuer: token_issuer.clone(),
            currency: token_currency.clone(),
            sending_precision: -15,
            max_holding_amount: Uint128::new(100),
            bridging_fee: Uint128::zero(),
        };
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            2,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            xrpl_base_fee,
        );

        // We successfully recover 3 tickets to perform operations
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
                },
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // We perform the register token operation, which should put the token to Processing state and create the PendingOperation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: token.issuer.clone(),
                currency: token.currency.clone(),
                sending_precision: token.sending_precision,
                max_holding_amount: token.max_holding_amount,
                bridging_fee: token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // If we try to recover a token that is not in Inactive state, it should fail.
        let recover_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverXRPLTokenRegistration {
                    issuer: token.issuer.clone(),
                    currency: token.currency.clone(),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_error
            .to_string()
            .contains(ContractError::XRPLTokenNotInactive {}.to_string().as_str()));

        // If we try to recover a token that is not registered, it should fail
        let recover_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RecoverXRPLTokenRegistration {
                    issuer: token.issuer.clone(),
                    currency: "NOT".to_string(),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(recover_error
            .to_string()
            .contains(ContractError::TokenNotRegistered {}.to_string().as_str()));

        // Let's fail the trust set operation to put the token to Inactive so that we can recover it

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: None,
                    ticket_sequence: Some(
                        query_pending_operations.operations[0]
                            .ticket_sequence
                            .unwrap(),
                    ),
                    transaction_result: TransactionResult::Rejected,
                    operation_result: None,
                },
            },
            &[],
            &signer,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert!(query_pending_operations.operations.is_empty());

        // We should be able to recover the token now
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverXRPLTokenRegistration {
                issuer: token.issuer.clone(),
                currency: token.currency.clone(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(
                    query_pending_operations.operations[0]
                        .ticket_sequence
                        .unwrap()
                ),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::TrustSet {
                    issuer: token_issuer,
                    currency: token_currency,
                    trust_set_limit_amount: Uint128::new(TRUST_SET_LIMIT_AMOUNT),
                },
                xrpl_base_fee,
            }
        );
    }

    #[test]
    fn rejected_ticket_allocation_with_no_tickets_left() {
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

        let test_tokens = vec![
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "USD".to_string(),     // Valid standard currency code
                sending_precision: -15,
                max_holding_amount: Uint128::new(100),
                bridging_fee: Uint128::zero(),
            },
            XRPLToken {
                issuer: generate_xrpl_address(), // Valid issuer
                currency: "015841551A748AD2C1F76FF6ECB0CCCD00000000".to_string(), // Valid hexadecimal currency
                sending_precision: 15,
                max_holding_amount: Uint128::new(50000),
                bridging_fee: Uint128::zero(),
            },
        ];
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            2,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            xrpl_base_fee,
        );

        // We successfully recover 3 tickets
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..4).collect()),
                    }),
                },
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // We register and enable 2 tokens, which should trigger a second ticket allocation with the last available ticket.
        for (index, token) in test_tokens.iter().enumerate() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: token.issuer.clone(),
                    currency: token.currency.clone(),
                    sending_precision: token.sending_precision,
                    max_holding_amount: token.max_holding_amount,
                    bridging_fee: token.bridging_fee,
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap();

            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(generate_hash()),
                        account_sequence: None,
                        ticket_sequence: Some(u64::try_from(index).unwrap() + 1),
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &[],
                &signer,
            )
            .unwrap();
        }

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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

        assert_eq!(
            query_pending_operations.operations,
            [Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(3),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::AllocateTickets { number: 2 },
                xrpl_base_fee,
            }]
        );
        assert_eq!(query_available_tickets.tickets, Vec::<u64>::new());

        // If we reject this operation, it should trigger a new ticket allocation but since we have no tickets available, it should
        // NOT fail (because otherwise contract will be stuck) but return an additional attribute warning that there are no available tickets left
        // requiring a manual ticket recovery in the future.
        let result = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(generate_hash()),
                        account_sequence: None,
                        ticket_sequence: Some(3),
                        transaction_result: TransactionResult::Rejected,
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: None,
                        }),
                    },
                },
                &vec![],
                &signer,
            )
            .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
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

        assert!(query_pending_operations.operations.is_empty());
        assert!(query_available_tickets.tickets.is_empty());
        assert!(result.events.iter().any(|e| e.ty == "wasm"
            && e.attributes
                .iter()
                .any(|a| a.key == "adding_ticket_allocation_operation_success"
                    && a.value == false.to_string())));
    }

    #[test]
    fn ticket_return_invalid_transactions() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let sender = accounts.get(1).unwrap();
        let relayer_account = accounts.get(2).unwrap();
        let relayer = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let xrpl_receiver_address = generate_xrpl_address();
        let bridge_xrpl_address = generate_xrpl_address();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            5,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            10,
        );

        // Add enough tickets to test that ticket is correctly returned

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(6),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..7).collect()),
                    }),
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Let's issue a token and register it
        let asset_ft = AssetFT::new(&app);
        let symbol = "TEST".to_string();
        let subunit = "utest".to_string();
        let decimals = 6;
        let initial_amount = Uint128::new(100000000);
        asset_ft
            .issue(
                MsgIssue {
                    issuer: sender.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &sender,
            )
            .unwrap();

        let denom = format!("{}-{}", subunit, sender.address()).to_lowercase();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: denom.clone(),
                decimals,
                sending_precision: 6,
                max_holding_amount: Uint128::new(10000000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // We are going to bridge a token and reject the operation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(1, denom.clone()),
            &sender,
        )
        .unwrap();

        // Get the current ticket used to compare later
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let ticket_used_invalid_operation = query_pending_operations.operations[0]
            .ticket_sequence
            .unwrap();

        // Send evidence of invalid operation, which should return the ticket to the ticket array
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: None,
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Invalid,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Now let's try to send again and verify that the ticket is the same as before (it was given back)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(1, denom.clone()),
            &sender,
        )
        .unwrap();

        // Get the current ticket used to compare later
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(
            ticket_used_invalid_operation,
            query_pending_operations.operations[0]
                .ticket_sequence
                .unwrap()
        );
    }

    #[test]
    fn token_update() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let xrpl_addresses: Vec<String> = (0..2).map(|_| generate_xrpl_address()).collect();
        let xrpl_pub_keys: Vec<String> = (0..2).map(|_| generate_xrpl_pub_key()).collect();

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
            generate_xrpl_address(),
            10,
        );

        // Recover enough tickets for testing
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: Some((1..6).collect()),
                        }),
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // Register one XRPL token and one Coreum token
        let xrpl_token = XRPLToken {
            issuer: generate_xrpl_address(),
            currency: "USD".to_string(),
            sending_precision: 15,
            max_holding_amount: Uint128::new(1000000000),
            bridging_fee: Uint128::zero(),
        };

        let subunit = "utest".to_string();
        asset_ft
            .issue(
                MsgIssue {
                    issuer: signer.address(),
                    symbol: "TEST".to_string(),
                    subunit: subunit.clone(),
                    precision: 6,
                    initial_amount: "100000000".to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "0".to_string(),
                    send_commission_rate: "0".to_string(),
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &signer,
            )
            .unwrap();

        let coreum_token_denom = format!("{}-{}", subunit, signer.address()).to_lowercase();

        let coreum_token = CoreumToken {
            denom: coreum_token_denom.clone(),
            decimals: 6,
            sending_precision: 6,
            max_holding_amount: Uint128::new(1000000000),
            bridging_fee: Uint128::zero(),
        };

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                sending_precision: xrpl_token.sending_precision,
                max_holding_amount: xrpl_token.max_holding_amount,
                bridging_fee: xrpl_token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let xrpl_token_denom = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.issuer == xrpl_token.issuer && t.currency == xrpl_token.currency)
            .unwrap()
            .coreum_denom
            .clone();

        // If we try to update the status of a token that is in processing state, it should fail
        let update_status_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateXRPLToken {
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    state: Some(TokenState::Disabled),
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(update_status_error
            .to_string()
            .contains(ContractError::TokenStateIsImmutable {}.to_string().as_str()));

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: None,
                        ticket_sequence: Some(1),
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // We will try to send one evidence with the token enabled and the other one with the token disabled, which should fail.
        let tx_hash = generate_hash();
        // First evidence should succeed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Disable the token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: Some(TokenState::Disabled),
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we send second evidence it should fail because token is disabled
        let disabled_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: xrpl_token.issuer.clone(),
                        currency: xrpl_token.currency.clone(),
                        amount: Uint128::one(),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(disabled_error
            .to_string()
            .contains(ContractError::TokenNotEnabled {}.to_string().as_str()));

        // If we try to change the status to something that is not disabled or enabled it should fail
        let update_status_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateXRPLToken {
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    state: Some(TokenState::Inactive),
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(update_status_error.to_string().contains(
            ContractError::InvalidTargetTokenState {}
                .to_string()
                .as_str()
        ));

        // If we try to change the status back to enabled and send the evidence, the balance should be sent to the receiver.
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: Some(TokenState::Enabled),
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "1".to_string());

        // If we disable again and we try to send the token back it will fail
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: Some(TokenState::Disabled),
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let send_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1, xrpl_token_denom.clone()),
                &signer,
            )
            .unwrap_err();

        assert!(send_error
            .to_string()
            .contains(ContractError::TokenNotEnabled {}.to_string().as_str()));

        // Register the Coreum Token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: coreum_token_denom.clone(),
                decimals: coreum_token.decimals,
                sending_precision: coreum_token.sending_precision,
                max_holding_amount: coreum_token.max_holding_amount,
                bridging_fee: coreum_token.bridging_fee,
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // If we try to change the status to something that is not disabled or enabled it should fail
        let update_status_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateCoreumToken {
                    denom: coreum_token_denom.clone(),
                    state: Some(TokenState::Processing),
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(update_status_error.to_string().contains(
            ContractError::InvalidTargetTokenState {}
                .to_string()
                .as_str()
        ));

        // Disable the Coreum Token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateCoreumToken {
                denom: coreum_token_denom.clone(),
                state: Some(TokenState::Disabled),
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we try to send now it will fail because the token is disabled
        let send_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1, coreum_token_denom.clone()),
                &signer,
            )
            .unwrap_err();

        assert!(send_error
            .to_string()
            .contains(ContractError::TokenNotEnabled {}.to_string().as_str()));

        // Enable it again and modify the sending precision
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateCoreumToken {
                denom: coreum_token_denom.clone(),
                state: Some(TokenState::Enabled),
                sending_precision: Some(5),
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Get the token information
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_coreum_tokens.tokens[0].sending_precision, 5);

        // If we try to update to an invalid sending precision it should fail
        let update_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateCoreumToken {
                    denom: coreum_token_denom.clone(),
                    state: None,
                    sending_precision: Some(7),
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(update_error.to_string().contains(
            ContractError::InvalidSendingPrecision {}
                .to_string()
                .as_str()
        ));

        // We will send 1 token and then modify the sending precision which should not allow the token to be sent with second evidence

        // Enable the token again (it was disabled)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: Some(TokenState::Enabled),
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        // First evidence should succeed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Let's update the sending precision from 15 to 14
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: Some(14),
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let evidence_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: xrpl_token.issuer.clone(),
                        currency: xrpl_token.currency.clone(),
                        amount: Uint128::one(),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(evidence_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // If we put it back to 15 and send, it should go through
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: Some(15),
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        // Let's send a bigger amount and check that it is truncated correctly after updating the sending precision
        let tx_hash = generate_hash();

        let previous_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();
        let amount_to_send = 100001; // This should truncate 1 after updating sending precision and send 100000

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Let's update the sending precision from 15 to 10
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: Some(10),
                bridging_fee: None,
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        let new_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();

        assert_eq!(
            new_balance.balance.parse::<u128>().unwrap(),
            previous_balance
                .balance
                .parse::<u128>()
                .unwrap()
                .checked_add(amount_to_send)
                .unwrap()
                .checked_sub(1) // Truncated amount after updating sending precision
                .unwrap()
        );

        // Updating bridging fee for Coreum Token should work
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateCoreumToken {
                denom: coreum_token_denom.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: Some(Uint128::new(1000)),
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Get the token information
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(
            query_coreum_tokens.tokens[0].bridging_fee,
            Uint128::new(1000)
        );

        // Let's send an XRPL token evidence, modify the bridging fee, check that it's updated, and send the next evidence to see that bridging fee is applied correctly
        let amount_to_send = 1000000;

        let tx_hash = generate_hash();
        // First evidence should succeed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Let's update the bridging fee from 0 to 10000000
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: Some(Uint128::new(10000000)),
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we try to send the second evidence it should fail because we can't cover new updated bridging fee
        let bridging_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: xrpl_token.issuer.clone(),
                        currency: xrpl_token.currency.clone(),
                        amount: Uint128::new(amount_to_send),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(bridging_error.to_string().contains(
            ContractError::CannotCoverBridgingFees {}
                .to_string()
                .as_str()
        ));

        // Let's update the bridging fee from 0 to 100000
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: Some(Uint128::new(1000000)),
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we try to send the second evidence it should fail because amount is 0 after applying bridging fees
        let bridging_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: xrpl_token.issuer.clone(),
                        currency: xrpl_token.currency.clone(),
                        amount: Uint128::new(amount_to_send),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(bridging_error.to_string().contains(
            ContractError::AmountSentIsZeroAfterTruncation {}
                .to_string()
                .as_str()
        ));

        // Let's update the bridging fee from 0 to 1000
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: Some(Uint128::new(1000)),
                max_holding_amount: None,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Sending evidence should succeed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        let previous_balance = new_balance;
        let new_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();

        assert_eq!(
            new_balance.balance.parse::<u128>().unwrap(),
            previous_balance
                .balance
                .parse::<u128>()
                .unwrap()
                .checked_add(amount_to_send) // 1000000 - 1000 (bridging fee) = 999000
                .unwrap()
                .checked_sub(1000) // bridging fee
                .unwrap()
                .checked_sub(99000) // Truncated amount after applying bridging fees (sending precision is 10) = 999000 -> 900000
                .unwrap()
        );

        // Let's bridge some tokens from Coreum to XRPL to have some amount in the bridge
        let current_max_amount = 10000;
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(current_max_amount, coreum_token_denom.clone()),
            &signer,
        )
        .unwrap();

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: None,
                        ticket_sequence: Some(
                            query_pending_operations.operations[0]
                                .ticket_sequence
                                .unwrap(),
                        ),
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: coreum_token_denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, current_max_amount.to_string());

        // Updating max holding amount for Coreum Token should work with less than current holding amount should not work
        let error_update = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateCoreumToken {
                    denom: coreum_token_denom.clone(),
                    state: None,
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: Some(Uint128::new(current_max_amount - 1)),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(error_update.to_string().contains(
            ContractError::InvalidTargetMaxHoldingAmount {}
                .to_string()
                .as_str()
        ));

        // Updating max holding amount with more than current holding amount should work
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateCoreumToken {
                denom: coreum_token_denom.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: Some(Uint128::new(current_max_amount + 1)),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(
            query_coreum_tokens.tokens[0].max_holding_amount,
            Uint128::new(current_max_amount + 1)
        );

        // Let's send an XRPL token evidence, modify the max_holding_amount, check that it's updated, and send the next evidence to see
        // that max_holding_amount checks are applied correctly

        // Get current bridged amount
        let bank = Bank::new(&app);
        let total_supplies = bank
            .query_total_supply(&QueryTotalSupplyRequest { pagination: None })
            .unwrap();

        let mut current_bridged_amount = 0;
        for total_supply in total_supplies.supply.iter() {
            if total_supply.denom == xrpl_token_denom {
                current_bridged_amount = total_supply.amount.clone().parse::<u128>().unwrap();
                break;
            }
        }

        // Let's update the max holding amount with current bridged amount - 1 (it should fail)
        let update_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateXRPLToken {
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    state: None,
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: Some(Uint128::new(current_bridged_amount - 1)),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(update_error.to_string().contains(
            ContractError::InvalidTargetMaxHoldingAmount {}
                .to_string()
                .as_str()
        ));

        // Let's send the first XRPL transfer evidence
        let amount_to_send = 1001000;

        let tx_hash = generate_hash();
        // First evidence should succeed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[0],
        )
        .unwrap();

        // Let's update the max holding amount with current bridged amount + amount to send - 1 (it should fail in next evidence send because it won't be enough)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: Some(Uint128::new(current_bridged_amount + amount_to_send - 1)),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we try to send the second evidence it should fail because we can't go over max holding amount
        let bridging_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash.clone(),
                        issuer: xrpl_token.issuer.clone(),
                        currency: xrpl_token.currency.clone(),
                        amount: Uint128::new(amount_to_send),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &[],
                relayer_accounts[1],
            )
            .unwrap_err();

        assert!(bridging_error.to_string().contains(
            ContractError::MaximumBridgedAmountReached {}
                .to_string()
                .as_str()
        ));

        // Get previous balance of user to compare later
        let previous_balance_user = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();

        // Let's update the max holding amount with current bridged amount + amount to send (second evidence should go through)
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLToken {
                issuer: xrpl_token.issuer.clone(),
                currency: xrpl_token.currency.clone(),
                state: None,
                sending_precision: None,
                bridging_fee: None,
                max_holding_amount: Some(Uint128::new(current_bridged_amount + amount_to_send)),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash.clone(),
                    issuer: xrpl_token.issuer.clone(),
                    currency: xrpl_token.currency.clone(),
                    amount: Uint128::new(amount_to_send),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &[],
            relayer_accounts[1],
        )
        .unwrap();

        let new_balance_user = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: signer.address(),
                denom: xrpl_token_denom.clone(),
            })
            .unwrap();

        // Check balance has been sent to user
        assert_eq!(
            new_balance_user.balance.parse::<u128>().unwrap(),
            previous_balance_user
                .balance
                .parse::<u128>()
                .unwrap()
                .checked_add(amount_to_send)
                .unwrap()
                .checked_sub(1000) // bridging fee
                .unwrap()
        );
    }

    #[test]
    fn test_burning_rate_and_commission_fee_coreum_tokens() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let relayer_account = accounts.get(1).unwrap();
        let sender = accounts.get(2).unwrap();
        let relayer = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let xrpl_receiver_address = generate_xrpl_address();
        let bridge_xrpl_address = generate_xrpl_address();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);

        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            9,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            10,
        );

        // Add enough tickets for all our test operations

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(10),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..11).collect()),
                    }),
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Let's issue a token with burning and commission fees and make sure it works out of the box
        let asset_ft = AssetFT::new(&app);
        let symbol = "TEST".to_string();
        let subunit = "utest".to_string();
        let decimals = 6;
        let initial_amount = Uint128::new(10000000000);
        asset_ft
            .issue(
                MsgIssue {
                    issuer: signer.address(),
                    symbol,
                    subunit: subunit.clone(),
                    precision: decimals,
                    initial_amount: initial_amount.to_string(),
                    description: "description".to_string(),
                    features: vec![MINTING as i32],
                    burn_rate: "1000000000000000000".to_string(), // 1e18 = 100%
                    send_commission_rate: "1000000000000000000".to_string(), // 1e18 = 100%
                    uri: "uri".to_string(),
                    uri_hash: "uri_hash".to_string(),
                },
                &signer,
            )
            .unwrap();

        let denom = format!("{}-{}", subunit, signer.address()).to_lowercase();

        // Let's transfer some tokens to a sender from the issuer so that we can check both rates being applied
        let bank = Bank::new(&app);
        bank.send(
            MsgSend {
                from_address: signer.address(),
                to_address: sender.address(),
                amount: vec![BaseCoin {
                    amount: "100000000".to_string(),
                    denom: denom.to_string(),
                }],
            },
            &signer,
        )
        .unwrap();

        // Check the balance
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "100000000".to_string());

        // Let's try to bridge some tokens and back and check that everything works correctly
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: denom.clone(),
                decimals,
                sending_precision: 6,
                max_holding_amount: Uint128::new(1000000000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: xrpl_receiver_address.clone(),
                deliver_amount: None,
            },
            &coins(100, denom.clone()),
            &sender,
        )
        .unwrap();

        // This should have burned an extra 100 and charged 100 tokens as commission fee to the sender. Let's check just in case
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "99999700".to_string());

        // Let's check that only 100 tokens are in the contract
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "100".to_string());

        // Let's confirm the briding XRPL and bridge the entire amount back to Coreum
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();
        assert_eq!(query_pending_operations.operations.len(), 1);

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: query_pending_operations.operations[0].account_sequence,
                    ticket_sequence: query_pending_operations.operations[0].ticket_sequence,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Get the token information
        let query_coreum_tokens = wasm
            .query::<QueryMsg, CoreumTokensResponse>(
                &contract_addr,
                &QueryMsg::CoreumTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let coreum_originated_token = query_coreum_tokens
            .tokens
            .iter()
            .find(|t| t.denom == denom)
            .unwrap();

        let amount_to_send_back = Uint128::new(100_000_000_000); // 100 utokens on Coreum are represented as 1e11 on XRPL
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: generate_hash(),
                    issuer: bridge_xrpl_address.clone(),
                    currency: coreum_originated_token.xrpl_currency.clone(),
                    amount: amount_to_send_back.clone(),
                    recipient: Addr::unchecked(sender.address()),
                },
            },
            &[],
            relayer_account,
        )
        .unwrap();

        // Check that the sender received the correct amount (100 tokens) and contract doesn't have anything left
        // This way we confirm that contract is not affected by commission fees and burn rate
        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: sender.address(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "99999800".to_string());

        let request_balance = asset_ft
            .query_balance(&QueryBalanceRequest {
                account: contract_addr.clone(),
                denom: denom.clone(),
            })
            .unwrap();

        assert_eq!(request_balance.balance, "0".to_string());
    }

    #[test]
    fn key_rotation() {
        let app = CoreumTestApp::new();
        let accounts_number = 4;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let xrpl_addresses: Vec<String> = (0..3).map(|_| generate_xrpl_address()).collect();
        let xrpl_pub_keys: Vec<String> = (0..3).map(|_| generate_xrpl_pub_key()).collect();

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
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![
                relayers[0].clone(),
                relayers[1].clone(),
                relayers[2].clone(),
            ],
            3,
            4,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            xrpl_base_fee,
        );

        // Recover enough tickets for testing
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(5),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: Some((1..6).collect()),
                        }),
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // Let's send a random evidence from 1 relayer that will stay after key rotation to confirm that it will be cleared after key rotation confirmation
        let tx_hash_old_evidence = generate_hash();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash_old_evidence.clone(),
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &vec![],
            &relayer_accounts[0],
        )
        .unwrap();

        // If we send it again it should by same relayer it should fail because it's duplicated
        let error_duplicated_evidence = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash_old_evidence.clone(),
                        issuer: XRP_ISSUER.to_string(),
                        currency: XRP_CURRENCY.to_string(),
                        amount: Uint128::one(),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &vec![],
                &relayer_accounts[0],
            )
            .unwrap_err();

        assert!(error_duplicated_evidence.to_string().contains(
            ContractError::EvidenceAlreadyProvided {}
                .to_string()
                .as_str()
        ));

        // We are going to perform a key rotation, for that we are going to remove a malicious relayer
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RotateKeys {
                new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                new_evidence_threshold: 2,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // If we try to perform another key rotation, it should fail because we have one pending ongoing
        let pending_rotation_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RotateKeys {
                    new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                    new_evidence_threshold: 2,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(pending_rotation_error
            .to_string()
            .contains(ContractError::RotateKeysOngoing {}.to_string().as_str()));

        // Let's confirm that a pending operation is created
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(1),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::RotateKeys {
                    new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                    new_evidence_threshold: 2
                },
                xrpl_base_fee,
            }
        );

        // Any evidence we send now that is not a RotateKeys evidence should fail
        let error_no_key_rotation_evidence = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash_old_evidence.clone(),
                        issuer: XRP_ISSUER.to_string(),
                        currency: XRP_CURRENCY.to_string(),
                        amount: Uint128::one(),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &vec![],
                &relayer_accounts[1],
            )
            .unwrap_err();

        assert!(error_no_key_rotation_evidence
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // We are going to confirm the RotateKeys as rejected and check that nothing is changed and bridge is still halted
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: None,
                        ticket_sequence: Some(1),
                        transaction_result: TransactionResult::Rejected,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // Pending operation should have been removed
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert!(query_pending_operations.operations.is_empty());

        // Check config and see that it's the same as before and bridge is still halted
        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(query_config.relayers, relayers);
        assert_eq!(query_config.evidence_threshold, 3);
        assert_eq!(query_config.bridge_state, BridgeState::Halted);

        // Let's try to perform a key rotation again and check that it works
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RotateKeys {
                new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                new_evidence_threshold: 2,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Let's confirm that a pending operation is created
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(2),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::RotateKeys {
                    new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                    new_evidence_threshold: 2
                },
                xrpl_base_fee,
            }
        );

        // We are going to confirm the RotateKeys as accepted and check that config has been updated correctly
        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: None,
                        ticket_sequence: Some(2),
                        transaction_result: TransactionResult::Accepted,
                        operation_result: None,
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(
            query_config.relayers,
            vec![relayers[0].clone(), relayers[1].clone()]
        );
        assert_eq!(query_config.evidence_threshold, 2);
        assert_eq!(query_config.bridge_state, BridgeState::Halted);

        // Owner can now resume the bridge
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ResumeBridge {},
            &vec![],
            &signer,
        )
        .unwrap();

        // Let's check that evidences have been cleared by sending again the old evidence and it succeeds
        // If evidences were cleared, this message will succeed because the evidence is not stored
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLToCoreumTransfer {
                    tx_hash: tx_hash_old_evidence.clone(),
                    issuer: XRP_ISSUER.to_string(),
                    currency: XRP_CURRENCY.to_string(),
                    amount: Uint128::one(),
                    recipient: Addr::unchecked(signer.address()),
                },
            },
            &vec![],
            &relayer_accounts[0],
        )
        .unwrap();

        // Finally, let's check that the old relayer can not send evidences anymore
        let error_not_relayer = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLToCoreumTransfer {
                        tx_hash: tx_hash_old_evidence.clone(),
                        issuer: XRP_ISSUER.to_string(),
                        currency: XRP_CURRENCY.to_string(),
                        amount: Uint128::one(),
                        recipient: Addr::unchecked(signer.address()),
                    },
                },
                &vec![],
                &relayer_accounts[2],
            )
            .unwrap_err();

        assert!(error_not_relayer
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));
    }

    #[test]
    fn bridge_halting_and_resuming() {
        let app = CoreumTestApp::new();
        let accounts_number = 3;
        let accounts = app
            .init_accounts(&coins(100_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get(0).unwrap();
        let relayer_account = accounts.get(1).unwrap();
        let new_relayer_account = accounts.get(2).unwrap();
        let relayer = Relayer {
            coreum_address: Addr::unchecked(relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let bridge_xrpl_address = generate_xrpl_address();

        let wasm = Wasm::new(&app);
        let asset_ft = AssetFT::new(&app);
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            9,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            bridge_xrpl_address.clone(),
            xrpl_base_fee,
        );

        // Halt the bridge and check that we can't send any operations except allowed ones
        wasm.execute::<ExecuteMsg>(&contract_addr, &ExecuteMsg::HaltBridge {}, &vec![], &signer)
            .unwrap();

        // Query bridge state to confirm it's halted
        let query_bridge_state = wasm
            .query::<QueryMsg, BridgeStateResponse>(&contract_addr, &QueryMsg::BridgeState {})
            .unwrap();

        assert_eq!(query_bridge_state.state, BridgeState::Halted);

        // Setting up some tickets should be allowed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(10),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..11).collect()),
                    }),
                },
            },
            &vec![],
            &relayer_account,
        )
        .unwrap();

        // Trying to register tokens should fail
        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterCoreumToken {
                    denom: "any_denom".to_string(),
                    decimals: 6,
                    sending_precision: 1,
                    max_holding_amount: Uint128::one(),
                    bridging_fee: Uint128::zero(),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: generate_xrpl_address(),
                    currency: "USD".to_string(),
                    sending_precision: 4,
                    max_holding_amount: Uint128::new(50000),
                    bridging_fee: Uint128::zero(),
                },
                &query_issue_fee(&asset_ft),
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Sending from Coreum to XRPL should fail
        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1, FEE_DENOM),
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Updating tokens should fail too
        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateXRPLToken {
                    issuer: "any_issuer".to_string(),
                    currency: "any_currency".to_string(),
                    state: Some(TokenState::Disabled),
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateCoreumToken {
                    denom: "any_denom".to_string(),
                    state: Some(TokenState::Disabled),
                    sending_precision: None,
                    bridging_fee: None,
                    max_holding_amount: None,
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Claiming pending refunds or relayers fees should fail
        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRefund {
                    pending_refund_id: "any_id".to_string(),
                },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        let bridge_halted_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ClaimRelayerFees {
                    amounts: vec![coin(1, FEE_DENOM)],
                },
                &[],
                relayer_account,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Resuming the bridge should work
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ResumeBridge {},
            &vec![],
            &signer,
        )
        .unwrap();

        // Query bridge state to confirm it's active
        let query_bridge_state = wasm
            .query::<QueryMsg, BridgeStateResponse>(&contract_addr, &QueryMsg::BridgeState {})
            .unwrap();

        assert_eq!(query_bridge_state.state, BridgeState::Active);

        // Halt it again to send some allowed operations
        wasm.execute::<ExecuteMsg>(&contract_addr, &ExecuteMsg::HaltBridge {}, &vec![], &signer)
            .unwrap();

        // Perform a simple key rotation, should be allowed
        let new_relayer = Relayer {
            coreum_address: Addr::unchecked(new_relayer_account.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        // We perform a key rotation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RotateKeys {
                new_relayers: vec![new_relayer.clone()],
                new_evidence_threshold: 1,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Let's query the pending operations to see that this operation was saved correctly
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);
        assert_eq!(
            query_pending_operations.operations[0],
            Operation {
                id: query_pending_operations.operations[0].id.clone(),
                version: 1,
                ticket_sequence: Some(1),
                account_sequence: None,
                signatures: vec![],
                operation_type: OperationType::RotateKeys {
                    new_relayers: vec![new_relayer.clone()],
                    new_evidence_threshold: 1
                },
                xrpl_base_fee,
            }
        );

        // Resuming now should not be allowed because we have a pending key rotation
        let resume_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::ResumeBridge {},
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(resume_error
            .to_string()
            .contains(ContractError::RotateKeysOngoing {}.to_string().as_str()));

        // Sending signatures should be allowed with the bridge halted and with pending operations
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveSignature {
                operation_id: 1,
                operation_version: 1,
                signature: "signature".to_string(),
            },
            &vec![],
            relayer_account,
        )
        .unwrap();

        // Sending an evidence for something that is not a RotateKeys should fail
        let bridge_halted_error = wasm
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
                &relayer_account,
            )
            .unwrap_err();

        assert!(bridge_halted_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Sending an evidence confirming a Key rotation should work and should also activate the bridge
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SaveEvidence {
                evidence: Evidence::XRPLTransactionResult {
                    tx_hash: Some(generate_hash()),
                    account_sequence: Some(1),
                    ticket_sequence: None,
                    transaction_result: TransactionResult::Accepted,
                    operation_result: None,
                },
            },
            &[],
            &relayer_account,
        )
        .unwrap();

        // Query bridge state to confirm it's still halted
        let query_bridge_state = wasm
            .query::<QueryMsg, BridgeStateResponse>(&contract_addr, &QueryMsg::BridgeState {})
            .unwrap();

        assert_eq!(query_bridge_state.state, BridgeState::Halted);

        // Query config to see that relayers have been correctly rotated
        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(query_config.relayers, vec![new_relayer]);

        // We should now be able to resume the bridge because the key rotation has been confirmed
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ResumeBridge {},
            &vec![],
            &signer,
        )
        .unwrap();

        // Query bridge state to confirm it's now active
        let query_bridge_state = wasm
            .query::<QueryMsg, BridgeStateResponse>(&contract_addr, &QueryMsg::BridgeState {})
            .unwrap();

        assert_eq!(query_bridge_state.state, BridgeState::Active);

        // Halt the bridge should not be possible by an address that is not owner or current relayer
        let halt_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::HaltBridge {},
                &vec![],
                &relayer_account,
            )
            .unwrap_err();

        assert!(halt_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        // Current relayer should be allowed to halt it
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::HaltBridge {},
            &vec![],
            &new_relayer_account,
        )
        .unwrap();

        let query_bridge_state = wasm
            .query::<QueryMsg, BridgeStateResponse>(&contract_addr, &QueryMsg::BridgeState {})
            .unwrap();

        assert_eq!(query_bridge_state.state, BridgeState::Halted);

        // Triggering a fee update during halted bridge should work
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLBaseFee { xrpl_base_fee: 600 },
            &vec![],
            &signer,
        )
        .unwrap();
    }

    #[test]
    fn updating_xrpl_base_fee() {
        let app = CoreumTestApp::new();
        let accounts_number = 4;
        let accounts = app
            .init_accounts(&coins(100_000_000_000_000, FEE_DENOM), accounts_number)
            .unwrap();

        let signer = accounts.get((accounts_number - 1) as usize).unwrap();
        let xrpl_addresses: Vec<String> = (0..3).map(|_| generate_xrpl_address()).collect();
        let xrpl_pub_keys: Vec<String> = (0..3).map(|_| generate_xrpl_pub_key()).collect();

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
        let xrpl_base_fee = 10;

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            relayers.clone(),
            3,
            9,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            xrpl_base_fee,
        );

        // Add enough tickets for all our tests
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(250),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let tx_hash = generate_hash();
        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveEvidence {
                    evidence: Evidence::XRPLTransactionResult {
                        tx_hash: Some(tx_hash.clone()),
                        account_sequence: Some(1),
                        ticket_sequence: None,
                        transaction_result: TransactionResult::Accepted,
                        operation_result: Some(OperationResult::TicketsAllocation {
                            tickets: Some((1..251).collect()),
                        }),
                    },
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // We are going to create the max number of pending operations and add signatures to them to verify that we can update all of them at once
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: generate_xrpl_address(),
                currency: "USD".to_string(),
                sending_precision: 15,
                max_holding_amount: Uint128::new(100000),
                bridging_fee: Uint128::zero(),
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // Register COREUM to send some
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: FEE_DENOM.to_string(),
                decimals: 6,
                sending_precision: 6,
                max_holding_amount: Uint128::new(100000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Let's create 247 more so that we get up to 250 in the end
        for _ in 0..247 {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SendToXRPL {
                    recipient: generate_xrpl_address(),
                    deliver_amount: None,
                },
                &coins(1, FEE_DENOM.to_string()),
                &signer,
            )
            .unwrap();
        }

        // Query pending operations with limit and start_after_key to verify it works
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: Some(100),
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 100);

        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: query_pending_operations.last_key,
                    limit: Some(200),
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 148);

        // Query all pending operations
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 248);

        // Halt the bridge to verify that we can't send signatures of pending operations that are not allowed
        let correct_signature_example = "3045022100DFA01DA5D6C9877F9DAA59A06032247F3D7ED6444EAD5C90A3AC33CCB7F19B3F02204D8D50E4D085BB1BC9DFB8281B8F35BDAEB7C74AE4B825F8CAE1217CFBDF4EA1".to_string();
        wasm.execute::<ExecuteMsg>(&contract_addr, &ExecuteMsg::HaltBridge {}, &vec![], &signer)
            .unwrap();

        let signature_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveSignature {
                    operation_id: query_pending_operations.operations[0]
                        .ticket_sequence
                        .unwrap(),
                    operation_version: 1,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                relayer_accounts[0],
            )
            .unwrap_err();

        assert!(signature_error
            .to_string()
            .contains(ContractError::BridgeHalted {}.to_string().as_str()));

        // Resume the bridge to add signatures again
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::ResumeBridge {},
            &vec![],
            &signer,
        )
        .unwrap();

        // Add some signatures to each pending operation
        for pending_operation in query_pending_operations.operations.iter() {
            for relayer in relayer_accounts.iter() {
                wasm.execute::<ExecuteMsg>(
                    &contract_addr,
                    &ExecuteMsg::SaveSignature {
                        operation_id: pending_operation.ticket_sequence.unwrap(),
                        operation_version: 1,
                        signature: correct_signature_example.clone(),
                    },
                    &vec![],
                    relayer,
                )
                .unwrap();
            }
        }

        // Add a Key Rotation, which will verify that we can update the base fee while the bridge is halted
        // and to check that we can add signatures for key rotations while bridge is halted
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RotateKeys {
                new_relayers: vec![relayers[0].clone(), relayers[1].clone()],
                new_evidence_threshold: 2,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Verify that we have 249 pending operations
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 249);

        // Sign this last operation with the 3 relayers

        for relayer in relayer_accounts.iter() {
            wasm.execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::SaveSignature {
                    operation_id: query_pending_operations.operations[248]
                        .ticket_sequence
                        .unwrap(),
                    operation_version: 1,
                    signature: correct_signature_example.clone(),
                },
                &vec![],
                relayer,
            )
            .unwrap();
        }

        // Verify that all pending operations are in version 1 and have three signatures each
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        for pending_operation in query_pending_operations.operations.iter() {
            assert_eq!(pending_operation.version, 1);
            assert_eq!(pending_operation.signatures.len(), 3);
        }

        // If we trigger an XRPL base fee by some who is not the owner, it should fail.
        let unauthorized_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::UpdateXRPLBaseFee { xrpl_base_fee: 600 },
                &vec![],
                &relayer_accounts[0],
            )
            .unwrap_err();

        assert!(unauthorized_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        let new_xrpl_base_fee = 20;
        // If we trigger an XRPL base fee update, all signatures must be gone, and pending operations must be in version 2, and pending operations base fee must be the new one
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::UpdateXRPLBaseFee {
                xrpl_base_fee: new_xrpl_base_fee,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Let's query all pending operations again to verify
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        for pending_operation in query_pending_operations.operations.iter() {
            assert_eq!(pending_operation.version, 2);
            assert_eq!(pending_operation.xrpl_base_fee, new_xrpl_base_fee);
            assert!(pending_operation.signatures.is_empty());
        }

        // Let's also verify that the XRPL base fee has been updated
        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(query_config.xrpl_base_fee, new_xrpl_base_fee);
    }

    #[test]
    fn cancel_pending_operation() {
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

        let new_relayer = Relayer {
            coreum_address: Addr::unchecked(not_owner.address()),
            xrpl_address: generate_xrpl_address(),
            xrpl_pub_key: generate_xrpl_pub_key(),
        };

        let contract_addr = store_and_instantiate(
            &wasm,
            &signer,
            Addr::unchecked(signer.address()),
            vec![relayer.clone()],
            1,
            3,
            Uint128::new(TRUST_SET_LIMIT_AMOUNT),
            query_issue_fee(&asset_ft),
            generate_xrpl_address(),
            10,
        );

        // Register COREUM Token
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterCoreumToken {
                denom: FEE_DENOM.to_string(),
                decimals: 6,
                sending_precision: 6,
                max_holding_amount: Uint128::new(1000000000000),
                bridging_fee: Uint128::zero(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Set up enough tickets
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(10),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Check that the ticket operation is there and cancel it
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);

        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::CancelPendingOperation {
                operation_id: query_pending_operations.operations[0]
                    .account_sequence
                    .unwrap(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Should be gone and no tickets allocated
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert!(query_pending_operations.operations.is_empty());

        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert!(query_available_tickets.tickets.is_empty());

        // This time we set them up correctly without cancelling
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RecoverTickets {
                account_sequence: 1,
                number_of_tickets: Some(10),
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
                    operation_result: Some(OperationResult::TicketsAllocation {
                        tickets: Some((1..11).collect()),
                    }),
                },
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Create 1 pending operation of each type
        // TrustSet pending operation
        let issuer = generate_xrpl_address();
        let currency = "USD".to_string();
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RegisterXRPLToken {
                issuer: issuer.clone(),
                currency: currency.clone(),
                sending_precision: 4,
                max_holding_amount: Uint128::new(50000),
                bridging_fee: Uint128::zero(),
            },
            &query_issue_fee(&asset_ft),
            &signer,
        )
        .unwrap();

        // CoreumToXRPLTransfer pending operation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::SendToXRPL {
                recipient: generate_xrpl_address(),
                deliver_amount: None,
            },
            &coins(1, FEE_DENOM.to_string()),
            &signer,
        )
        .unwrap();

        // RotateKeys operation
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::RotateKeys {
                new_relayers: vec![new_relayer.clone()],
                new_evidence_threshold: 1,
            },
            &vec![],
            &signer,
        )
        .unwrap();

        // Check that 3 tickets are currently being used
        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_available_tickets.tickets.len(), 7); // 10 - 3

        // Check that we have one of each pending operation types
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 3);

        // If someone that is not the owner tries to cancel it should fail
        let cancel_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::CancelPendingOperation {
                    operation_id: query_pending_operations.operations[0]
                        .ticket_sequence
                        .unwrap(),
                },
                &vec![],
                &not_owner,
            )
            .unwrap_err();

        assert!(cancel_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        // If owner tries to cancel a pending operation that does not exist it should fail
        let cancel_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::CancelPendingOperation { operation_id: 50 },
                &vec![],
                &signer,
            )
            .unwrap_err();

        assert!(cancel_error.to_string().contains(
            ContractError::PendingOperationNotFound {}
                .to_string()
                .as_str()
        ));

        // Cancel the first pending operation (trust set) and check that ticket is returned and token is put in Inactive state
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::CancelPendingOperation {
                operation_id: query_pending_operations.operations[0]
                    .ticket_sequence
                    .unwrap(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_xrpl_tokens = wasm
            .query::<QueryMsg, XRPLTokensResponse>(
                &contract_addr,
                &QueryMsg::XRPLTokens {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        let token = query_xrpl_tokens
            .tokens
            .iter()
            .find(|t| t.currency == currency && t.issuer == issuer)
            .unwrap();

        assert_eq!(token.state, TokenState::Inactive);

        // Check that 2 tickets are currently being used (1 has been returned)
        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_available_tickets.tickets.len(), 8);

        // Check that we the cancelled operation was removed
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 2);

        // Cancel the second pending operation (CoreumToXRPLTransfer), which should create a pending refund for the sender
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::CancelPendingOperation {
                operation_id: query_pending_operations.operations[0]
                    .ticket_sequence
                    .unwrap(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_pending_refunds = wasm
            .query::<QueryMsg, PendingRefundsResponse>(
                &contract_addr,
                &QueryMsg::PendingRefunds {
                    address: Addr::unchecked(signer.address()),
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_refunds.pending_refunds.len(), 1);
        assert_eq!(
            query_pending_refunds.pending_refunds[0].coin,
            coin(1, FEE_DENOM)
        );

        // Check that 1 tickets is currently being used (2 have been returned)
        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_available_tickets.tickets.len(), 9);

        // Check that we the cancelled operation was removed
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert_eq!(query_pending_operations.operations.len(), 1);

        // Cancel the RotateKeys operation, it should keep the bridge halted and not rotate the relayers
        wasm.execute::<ExecuteMsg>(
            &contract_addr,
            &ExecuteMsg::CancelPendingOperation {
                operation_id: query_pending_operations.operations[0]
                    .ticket_sequence
                    .unwrap(),
            },
            &vec![],
            &signer,
        )
        .unwrap();

        let query_config = wasm
            .query::<QueryMsg, Config>(&contract_addr, &QueryMsg::Config {})
            .unwrap();

        assert_eq!(query_config.bridge_state, BridgeState::Halted);
        assert_eq!(query_config.relayers, vec![relayer]);

        // This should have returned all tickets and removed all pending operations from the queue
        // Check that all tickets are available (the 10 that we initially allocated)
        let query_available_tickets = wasm
            .query::<QueryMsg, AvailableTicketsResponse>(
                &contract_addr,
                &QueryMsg::AvailableTickets {},
            )
            .unwrap();

        assert_eq!(query_available_tickets.tickets.len(), 10);

        // Check that we the cancelled operation was removed
        let query_pending_operations = wasm
            .query::<QueryMsg, PendingOperationsResponse>(
                &contract_addr,
                &QueryMsg::PendingOperations {
                    start_after_key: None,
                    limit: None,
                },
            )
            .unwrap();

        assert!(query_pending_operations.operations.is_empty());
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
            generate_xrpl_address(),
            10,
        );

        let tx_hash = generate_hash();
        let account_sequence = 1;
        let tickets: Vec<u64> = (1..6).collect();

        let invalid_evidences_input = vec![
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: None,
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: Some(2),
                transaction_result: TransactionResult::Rejected,
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: None,
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(tickets.clone()),
                }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(tx_hash.clone()),
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Invalid,
                operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: None,
                account_sequence: Some(account_sequence),
                ticket_sequence: None,
                transaction_result: TransactionResult::Invalid,
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(tickets),
                }),
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
            generate_xrpl_address(),
            10,
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
                    sending_precision: 1,
                    max_holding_amount: Uint128::one(),
                    bridging_fee: Uint128::zero(),
                },
                &vec![],
                &not_owner,
            )
            .unwrap_err();

        assert!(register_coreum_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

        // Try registering an XRPL token as not_owner, should fail
        let register_xrpl_error = wasm
            .execute::<ExecuteMsg>(
                &contract_addr,
                &ExecuteMsg::RegisterXRPLToken {
                    issuer: generate_xrpl_address(),
                    currency: "USD".to_string(),
                    sending_precision: 4,
                    max_holding_amount: Uint128::new(50000),
                    bridging_fee: Uint128::zero(),
                },
                &query_issue_fee(&asset_ft),
                &not_owner,
            )
            .unwrap_err();

        assert!(register_xrpl_error
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));

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

        assert!(recover_tickets
            .to_string()
            .contains(ContractError::UnauthorizedSender {}.to_string().as_str()));
    }

    #[test]
    fn enum_hashes() {
        let hash = generate_hash();
        let issuer = "issuer".to_string();
        let currency = "currency".to_string();
        let amount = Uint128::new(100);
        let recipient = Addr::unchecked("signer");

        // Create multiple evidences changing only 1 field to verify that all of them have different hashes
        let xrpl_to_coreum_transfer_evidences = vec![
            Evidence::XRPLToCoreumTransfer {
                tx_hash: hash.clone(),
                issuer: issuer.clone(),
                currency: currency.clone(),
                amount: amount.clone(),
                recipient: recipient.clone(),
            },
            Evidence::XRPLToCoreumTransfer {
                tx_hash: generate_hash(),
                issuer: issuer.clone(),
                currency: currency.clone(),
                amount: amount.clone(),
                recipient: recipient.clone(),
            },
            Evidence::XRPLToCoreumTransfer {
                tx_hash: hash.clone(),
                issuer: "new_issuer".to_string(),
                currency: currency.clone(),
                amount: amount.clone(),
                recipient: recipient.clone(),
            },
            Evidence::XRPLToCoreumTransfer {
                tx_hash: hash.clone(),
                issuer: issuer.clone(),
                currency: "new_currency".to_string(),
                amount: amount.clone(),
                recipient: recipient.clone(),
            },
            Evidence::XRPLToCoreumTransfer {
                tx_hash: hash.clone(),
                issuer: issuer.clone(),
                currency: currency.clone(),
                amount: Uint128::one(),
                recipient: recipient.clone(),
            },
            Evidence::XRPLToCoreumTransfer {
                tx_hash: hash.clone(),
                issuer: issuer.clone(),
                currency: currency.clone(),
                amount: amount.clone(),
                recipient: Addr::unchecked("new_recipient"),
            },
        ];

        // Add them all to a map to see that they create different entries
        let mut evidence_map = HashMap::new();
        for evidence in xrpl_to_coreum_transfer_evidences.iter() {
            evidence_map.insert(
                hash_bytes(serde_json::to_string(evidence).unwrap().into_bytes()),
                true,
            );
        }

        assert_eq!(evidence_map.len(), xrpl_to_coreum_transfer_evidences.len());

        let hash = Some(generate_hash());
        let operation_id = Some(1);
        let transaction_result = TransactionResult::Accepted;
        let operation_result = None;
        // Create multiple evidences changing only 1 field to verify that all of them have different hashes
        let xrpl_transaction_result_evidences = vec![
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: operation_id,
                ticket_sequence: None,
                transaction_result: transaction_result.clone(),
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: Some(generate_hash()),
                account_sequence: operation_id,
                ticket_sequence: None,
                transaction_result: transaction_result.clone(),
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: Some(2),
                ticket_sequence: None,
                transaction_result: transaction_result.clone(),
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: None,
                ticket_sequence: operation_id,
                transaction_result: transaction_result.clone(),
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: None,
                ticket_sequence: Some(2),
                transaction_result: transaction_result.clone(),
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: operation_id,
                ticket_sequence: None,
                transaction_result: TransactionResult::Rejected,
                operation_result: operation_result.clone(),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: operation_id,
                ticket_sequence: None,
                transaction_result: transaction_result.clone(),
                operation_result: Some(OperationResult::TicketsAllocation { tickets: None }),
            },
            Evidence::XRPLTransactionResult {
                tx_hash: hash.clone(),
                account_sequence: operation_id,
                ticket_sequence: None,
                transaction_result: transaction_result.clone(),
                operation_result: Some(OperationResult::TicketsAllocation {
                    tickets: Some(vec![1, 2, 3]),
                }),
            },
        ];

        // Add them all to a map to see that they create different entries
        let mut evidence_map = HashMap::new();
        for evidence in xrpl_transaction_result_evidences.iter() {
            evidence_map.insert(
                hash_bytes(serde_json::to_string(evidence).unwrap().into_bytes()),
                true,
            );
        }

        assert_eq!(evidence_map.len(), xrpl_transaction_result_evidences.len());
    }

    #[test]
    fn validate_xrpl_addresses() {
        let mut valid_addresses = vec![
            "rU6K7V3Po4snVhBBaU29sesqs2qTQJWDw1".to_string(),
            "rLUEXYuLiQptky37CqLcm9USQpPiz5rkpD".to_string(),
            "rBTwLga3i2gz3doX6Gva3MgEV8ZCD8jjah".to_string(),
            "rDxMt25DoKeNv7te7WmLvWwsmMyPVBctUW".to_string(),
            "rPbPkTSrAqANkoTFpwheTxRyT8EQ38U5ok".to_string(),
            "rQ3fNyLjbvcDaPNS4EAJY8aT9zR3uGk17c".to_string(),
            "rnATJKpFCsFGfEvMC3uVWHvCEJrh5QMuYE".to_string(),
            generate_xrpl_address(),
            generate_xrpl_address(),
            generate_xrpl_address(),
            generate_xrpl_address(),
        ];

        // Add the current prohibited recipients and check that they are valid generated xrpl addresses
        for prohibited_recipient in INITIAL_PROHIBITED_XRPL_RECIPIENTS {
            valid_addresses.push(prohibited_recipient.to_string());
        }

        for address in valid_addresses.iter() {
            validate_xrpl_address(address).unwrap();
        }

        let mut invalid_addresses = vec![
            "zDTXLQ7ZKZVKz33zJbHjgVShjsBnqMBhmN".to_string(), // Invalid prefix
            "rf1BiGeXwwQoi8Z2u".to_string(),                  // Too short
            "rU6K7V3Po4snVhBBaU29sesqs2qTQJWDw1hBBaU29".to_string(), // Too long
            "rU6K7V3Po4snVhBBa029sesqs2qTQJWDw1".to_string(), // Contains invalid character 0
            "rU6K7V3Po4snVhBBaU29sesql2qTQJWDw1".to_string(), // Contains invalid character l
            "rLUEXYuLiQptky37OqLcm9USQpPiz5rkpD".to_string(), // Contains invalid character O
            "rLUEXYuLiQpIky37CqLcm9USQpPiz5rkpD".to_string(), // Contains invalid character I
        ];

        for _ in 0..100 {
            invalid_addresses.push(generate_invalid_xrpl_address()); // Just random address without checksum calculation
        }

        for address in invalid_addresses.iter() {
            validate_xrpl_address(address).unwrap_err();
        }
    }
}
