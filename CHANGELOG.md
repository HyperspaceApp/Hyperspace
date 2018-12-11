Version History
---------------

Dec 11, 2018:

v0.2.3

- Added support for SPV renters
- Stopped generating extra transactions when building contracts
- Eliminated wallet automatic defrag code, wallet now defrags only when necessary
- Fixed an address gap limit bug when forming new contracts

Oct 28, 2018:

v0.2.2
- SPV bug fixes

Oct 28, 2018:

v0.2.1
- Functional SPV nodes
- Wallet bug fixes

Oct 04, 2018:

v0.2.0
- Updated the /wallet/transactions endpoint
- Added a /renter/contract/cancel endpoint
- File encryption uses Threefish instead of Twofish
- Added much header-only consensus processing code for SPV
- Many bug fixes from the beta

Sept 19, 2018:

v0.2.0-beta
- Full nodes now generate and maintain Golomb-coded set filters
- Wallets now generate addresses in accordance with an address gap limit
- Scanning uses GCS filters and address gap limits and is much faster
- Eliminated rescans
- Added a /wallet/build/transaction API endpoint
- Changed the /wallet/address [GET] API endpoint to a /wallet/address [POST] endpoint
- Added a new /wallet/address [GET] API endpoint to retrieve an address that has not been seen on the blockchain

July 2018:

v0.1.0: Closed beta release.
v0.1.1: Adjust genesis block mining difficulty.
