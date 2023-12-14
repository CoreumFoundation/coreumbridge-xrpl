use coreum_wasm_sdk::core::CoreumMsg;
use cosmwasm_std::{coin, BankMsg, Coin, Decimal, Response, Storage, Uint128};

use crate::{
    contract::XRPL_MIN_TRANSFER_RATE,
    error::ContractError,
    state::{CONFIG, FEES_COLLECTED},
};

pub fn amount_after_bridge_fees(
    amount: Uint128,
    bridging_fee: Uint128,
) -> Result<Uint128, ContractError> {
    let amount_after_bridge_fees = amount
        .checked_sub(bridging_fee)
        .map_err(|_| ContractError::CannotCoverBridgingFees {})?;

    Ok(amount_after_bridge_fees)
}

pub fn amount_after_transfer_fees(
    amount: Uint128,
    transfer_rate: Option<Uint128>,
) -> Result<(Uint128, Uint128), ContractError> {
    let mut amount_after_transfer_fees = amount;
    let mut transfer_fee = Uint128::zero();

    if let Some(rate) = transfer_rate {
        // The formula to calculate how much we can send is the following: amount_to_send = amount / (1 + fee_percentage)
        // Where, for 5% fee for example, fee_percentage is 0.05 and for 100% fee fee_percentage is 1.
        // To calculate the right amounts, first we get the rate from the XRPL transfer rate value
        // For example, if our transfer rate is 2% (1020000000), we will get 2% by doing 1020000000 - 1000000000 = 20000000
        // and then dividing this by 1000000000 to get the percentage (0.02)
        // Afterwards we just need to apply the formula to get the amount to send (rounded down) and substract from the initial amount to get the fee that is applied.
        let rate_value = rate.checked_sub(XRPL_MIN_TRANSFER_RATE)?;
        let rate_percentage = Decimal::from_ratio(rate_value, XRPL_MIN_TRANSFER_RATE);

        let denominator = Decimal::one().checked_add(rate_percentage)?;

        amount_after_transfer_fees = amount.div_floor(denominator);
        transfer_fee = amount.checked_sub(amount_after_transfer_fees)?;
    }

    Ok((amount_after_transfer_fees, transfer_fee))
}

pub fn handle_fee_collection(
    storage: &mut dyn Storage,
    bridging_fee: Uint128,
    token_denom: String,
    truncated_portion: Uint128,
) -> Result<Uint128, ContractError> {
    // We add the bridging fee we charged and the truncated portion after all fees were charged
    let fee_collected = bridging_fee.checked_add(truncated_portion)?;

    collect_fees(storage, coin(fee_collected.u128(), token_denom))?;
    Ok(fee_collected)
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

pub fn claim_fees_for_relayers(
    storage: &mut dyn Storage,
) -> Result<Response<CoreumMsg>, ContractError> {
    let mut fees_collected = FEES_COLLECTED.load(storage)?;
    let relayers = CONFIG.load(storage)?.relayers;
    let mut coins_for_each_relayer = vec![];

    for fee in fees_collected.iter_mut() {
        // For each token collected in fees, we will divide the amount by the number of relayers to know how much we need to send to each relayer
        let amount_for_each_relayer = fee
            .amount
            .u128()
            .checked_div(relayers.len() as u128)
            .unwrap();

        // If the amount is 0, we don't send it to the relayers
        if amount_for_each_relayer != 0 {
            coins_for_each_relayer.push(coin(amount_for_each_relayer, fee.denom.to_owned()));
        }

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
    // We need to do this step to avoid the posibility of iterating over them next claim
    fees_collected.retain(|c| !c.amount.is_zero());
    FEES_COLLECTED.save(storage, &fees_collected)?;

    Ok(Response::new().add_messages(send_messages))
}
