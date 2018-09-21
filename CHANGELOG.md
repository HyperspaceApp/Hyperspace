Version History
---------------

Sept 19, 2018:

v0.2.0
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
