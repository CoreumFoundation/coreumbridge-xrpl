use cosmwasm_std::{coin, Addr, Coin, Decimal, Storage, Uint128};

use crate::{
    contract::XRPL_ZERO_TRANSFER_RATE,
    error::ContractError,
    state::{CONFIG, FEES_COLLECTED, FEES_REMAINER},
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
        let rate_value = rate.checked_sub(XRPL_ZERO_TRANSFER_RATE)?;
        let rate_percentage = Decimal::from_ratio(rate_value, XRPL_ZERO_TRANSFER_RATE);

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
    remainder: Uint128,
) -> Result<Uint128, ContractError> {
    // We add the bridging fee we charged and the truncated portion after all fees were charged
    let fee_collected = bridging_fee.checked_add(remainder)?;

    collect_fees(storage, coin(fee_collected.u128(), token_denom))?;
    Ok(fee_collected)
}

fn collect_fees(storage: &mut dyn Storage, fee: Coin) -> Result<(), ContractError> {
    // We only collect fees if there is something to collect
    // If for some reason there is a coin that we are not charging fees for, we don't collect it
    if !fee.amount.is_zero() {
        let mut fees_remainer = FEES_REMAINER.load(storage)?;
        // We add the new fees to the possible remainers that we had before and use those amounts to allocate them to relayers
        let total_fee = match fees_remainer.iter_mut().find(|c| c.denom == fee.denom) {
            Some(coin) => {
                // We get the remainder and put it back to 0
                let total_fee = fee.amount + coin.amount;
                coin.amount = Uint128::zero();
                total_fee
            }
            None => fee.amount,
        };

        // We will divide the total fee by the number of relayers to know how much we need to send to each relayer and the remainder will be saved for the next fee collection
        let relayers = CONFIG.load(storage)?.relayers;
        let amount_for_each_relayer =
            total_fee.checked_div(Uint128::new(relayers.len().try_into().unwrap()))?;

        // If the amount is 0, there's nothing to send to the relayers
        if !amount_for_each_relayer.is_zero() {
            for relayer in relayers.iter() {
                // We get previous relayer fees collected to update them. If it's the first time the relayer gets fees, we initialize the array
                let mut fees_collected = FEES_COLLECTED
                    .may_load(storage, relayer.coreum_address.to_owned())?
                    .unwrap_or_default();

                // Add fees to the relayer fees collected
                match fees_collected.iter_mut().find(|c| c.denom == fee.denom) {
                    Some(coin) => coin.amount += amount_for_each_relayer,
                    None => fees_collected
                        .push(coin(amount_for_each_relayer.u128(), fee.denom.to_owned())),
                }

                FEES_COLLECTED.save(storage, relayer.coreum_address.to_owned(), &fees_collected)?;
            }
        }

        // We get the remainder in case there is one and save it for the next fee collection
        let remainer = total_fee.checked_sub(
            amount_for_each_relayer
                .checked_mul(Uint128::new(relayers.len().try_into().unwrap()))?,
        )?;

        // We save the remainer in the array of remainers
        match fees_remainer.iter_mut().find(|c| c.denom == fee.denom) {
            Some(coin) => coin.amount += remainer,
            None => fees_remainer.push(coin(remainer.u128(), fee.denom)),
        }

        // Remove everything that is 0 from the fees remainers array to avoid iterating over them next time we collect fees
        fees_remainer.retain(|c| !c.amount.is_zero());

        FEES_REMAINER.save(storage, &fees_remainer)?;
    }

    Ok(())
}

pub fn check_and_update_relayer_fees(
    storage: &mut dyn Storage,
    sender: Addr,
    amounts: &Vec<Coin>,
) -> Result<(), ContractError> {
    let mut fees_collected = FEES_COLLECTED.load(storage, sender.to_owned())?;
    // We are going to check that the amounts sent to claim are available in the fees collected and if they are, substract them
    // If they are not, we will return an error
    for coin in amounts {
        match fees_collected
            .iter_mut()
            .find(|f| f.denom == coin.denom && f.amount >= coin.amount)
        {
            Some(found_coin) => found_coin.amount -= coin.amount,
            None => {
                return Err(ContractError::RelayerFeeNotClaimable {
                    denom: coin.denom.to_owned(),
                    amount: coin.amount,
                })
            }
        }
    }

    // Clean if amount is zero
    fees_collected.retain(|c| !c.amount.is_zero());

    FEES_COLLECTED.save(storage, sender, &fees_collected)?;

    Ok(())
}
