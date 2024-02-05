use crate::error::ContractError;
use bs58::Alphabet;
use sha2::{Digest, Sha256};

pub fn validate_xrpl_address(address: String) -> Result<(), ContractError> {
    // We need to use the base58 dictionary for ripple which is rpshnaf39wBUDNEGHJKLM4PQRST7VWXYZ2bcdeCg65jkm8oFqi1tuvAxyz
    // To understand this alphabet, see https://xrpl.org/base58-encodings.html#ripple-base58-alphabet
    // In short, the alphabet represents the bytes values in the address. r = 0, p = 1, s = 2, etc.
    let data = bs58::decode(&address)
        .with_alphabet(Alphabet::RIPPLE)
        .into_vec()
        .map_err(|_| ContractError::InvalidXRPLAddress {
            address: address.to_owned(),
        })?;

    // An XRPL address, once decoded from its base58 representation, should be exactly 25 bytes long. 
    // This length is a standard for XRPL addresses and includes various components like the actual address, a version byte, and a checksum.
    // The first part of the address is usually a version byte ('r' which is 0 in the Base58 Alphabet for XRPL), 
    // followed by the 20-byte address itself, and then a 4-byte checksum at the end. The total is thus 1 + 20 + 4 = 25 bytes.
    // If the decoded data is not 25 bytes long, it's not a valid XRPL address.
    // If the first byte is not 0 ('r'), it's not a valid XRPL address.
    if data.len() != 25 || data[0] != 0 {
        return Err(ContractError::InvalidXRPLAddress { address });
    }

    // The checksum is the last 4 bytes of the decoded data.
    // Its a double SHA256 hash of the first 21 bytes of the decoded data.
    // For more info, see https://xrpl.org/addresses.html#address-encoding
    let expected_checksum = &checksum(&data[..21])[..4];
    let provided_checksum = &data[21..];

    if *expected_checksum != *provided_checksum {
        return Err(ContractError::InvalidXRPLAddress { address });
    }

    Ok(())
}

pub fn checksum(data: &[u8]) -> Vec<u8> {
    Sha256::digest(Sha256::digest(data)).to_vec()
}
