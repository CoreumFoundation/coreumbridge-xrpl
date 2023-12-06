use coreum_wasm_sdk::core::CoreumMsg;
use cosmwasm_std::{coin, BankMsg, Coin, Response, Storage, Uint128};

use crate::{error::ContractError, relayer::Relayer, state::FEES_COLLECTED};

pub fn handle_fee_collection(
    storage: &mut dyn Storage,
    amount: Uint128,
    bridging_fee: Uint128,
    token_denom: String,
    truncated_portion: Uint128,
) -> Result<Uint128, ContractError> {
    // We substract the truncated portion from the bridging_fee. If truncated portion >= fee,
    // then we already paid the fees and we collect the truncated portion instead of bridging fee (because it might be bigger than the bridging fee)
    let fee_to_collect = bridging_fee.saturating_sub(truncated_portion);
    if fee_to_collect.is_zero() {
        collect_fees(storage, coin(truncated_portion.u128(), token_denom))?;
    } else {
        collect_fees(storage, coin(bridging_fee.u128(), token_denom))?;
    }

    let amount_after_fees = amount
        .checked_sub(fee_to_collect)
        .map_err(|_| ContractError::CannotCoverBridgingFees {})?;

    Ok(amount_after_fees)
}

pub fn collect_fees(storage: &mut dyn Storage, fee: Coin) -> Result<(), ContractError> {
    // We only collect fees if there is something to collect
    // If for some reason there is a coin that we are not charging fees for, we don't collect it
    if !fee.amount.is_zero() {
        let mut fees_collected = FEES_COLLECTED.load(storage)?;
        // If we already have the coin in the fee collected array, we update the amount, if not, we add it as a new element.
        match fees_collected.iter_mut().find(|c| c.denom == fee.denom) {
            Some(coin) => coin.amount += fee.amount,
            None => fees_collected.push(fee),
        }
        FEES_COLLECTED.save(storage, &fees_collected)?;
    }

    Ok(())
}

pub fn claim_relayer_fees(
    storage: &mut dyn Storage,
    relayers: Vec<Relayer>,
) -> Result<Response<CoreumMsg>, ContractError> {
    let mut fees_collected = FEES_COLLECTED.load(storage)?;
    let mut coins_for_each_relayer = vec![];

    for fee in fees_collected.iter_mut() {
        // For each token collected in fees, we will divide the amount by the number of relayers to know how much we need to send to each relayer
        let amount_for_each_relayer = fee
            .amount
            .u128()
            .checked_div(relayers.len() as u128)
            .unwrap();

        coins_for_each_relayer.push(coin(amount_for_each_relayer, fee.denom.to_owned()));

        // We substract the amount we are sending to the relayers from the total amount collected
        // We can't simply remove it from the array because there might be small amounts left due to truncation when dividing
        fee.amount = fee
            .amount
            .checked_sub(Uint128::from(
                amount_for_each_relayer
                    .checked_mul(relayers.len() as u128)
                    .unwrap(),
            ))
            .unwrap();
    }

    // We'll have 1 multi send message for each relayer
    let mut send_messages = vec![];
    for relayer in relayers.iter() {
        send_messages.push(BankMsg::Send {
            to_address: relayer.coreum_address.to_string(),
            amount: coins_for_each_relayer.clone(),
        });
    }

    // Last thing we do is to clean the fees collected array removing the coins that have 0 amount
    // We need to do this step because if we keep them we will divide by 0 the next time we are iterating over the fees.
    fees_collected.retain(|c| !c.amount.is_zero());
    FEES_COLLECTED.save(storage, &fees_collected)?;

    Ok(Response::new().add_messages(send_messages))
}
