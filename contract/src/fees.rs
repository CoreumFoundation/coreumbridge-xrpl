use cosmwasm_std::{coin, Addr, Coin, Decimal, Storage, Uint128};

use crate::{
    error::ContractError,
    state::{CONFIG, FEES_COLLECTED, FEE_REMAINDERS},
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
        let fees_remainder = FEE_REMAINDERS.may_load(storage, fee.denom.to_owned())?;
        // We add the new fees to the possible remainders that we had before and use those amounts to allocate them to relayers
        let total_fee = match fees_remainder {
            Some(fees_remainder) => fee.amount.checked_add(fees_remainder)?,
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
        let remainder = total_fee.checked_sub(
            amount_for_each_relayer
                .checked_mul(Uint128::new(relayers.len().try_into().unwrap()))?,
        )?;

        // We save the remainder
        FEE_REMAINDERS.save(storage, fee.denom, &remainder)?;
    }

    Ok(())
}

pub fn substract_relayer_fees(
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
                return Err(ContractError::NotEnoughFeesToClaim {
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
