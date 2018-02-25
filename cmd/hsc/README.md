Siac Usage
==========

`hdcc` is the command line interface to Sia, for use by power users and
those on headless servers. It comes as a part of the command line
package, and can be run as `./hdcc` from the same folder, or just by
calling `hdcc` if you move the binary into your path.

Most of the following commands have online help. For example, executing
`hdcc wallet send help` will list the arguments for that command,
while `hdcc host help` will list the commands that can be called
pertaining to hosting. `hdcc help` will list all of the top level
command groups that can be used.

You can change the address of where hdcd is pointing using the `-a`
flag. For example, `hdcc -a :9000 status` will display the status of
the hdcd instance launched on the local machine with `hdcd -a :9000`.

Common tasks
------------
* `hdcc consensus` view block height

Wallet:
* `hdcc wallet init [-p]` initilize a wallet
* `hdcc wallet unlock` unlock a wallet
* `hdcc wallet balance` retrieve wallet balance
* `hdcc wallet address` get a wallet address
* `hdcc wallet send [amount] [dest]` sends hdccoin to an address

Renter:
* `hdcc renter list` list all renter files
* `hdcc renter upload [filepath] [nickname]` upload a file
* `hdcc renter download [nickname] [filepath]` download a file


Full Descriptions
-----------------

#### Wallet tasks

* `hdcc wallet init [-p]` encrypts and initializes the wallet. If the
`-p` flag is provided, an encryption password is requested from the
user. Otherwise the initial seed is used as the encryption
password. The wallet must be initialized and unlocked before any
actions can be performed on the wallet.

Examples:
```bash
user@hostname:~$ hdcc -a :9920 wallet init
Seed is:
 cider sailor incur sober feast unhappy mundane sadness hinder aglow imitate amaze duties arrow gigantic uttered inflamed girth myriad jittery hexagon nail lush reef sushi pastry southern inkling acquire

Wallet encrypted with password: cider sailor incur sober feast unhappy mundane sadness hinder aglow imitate amaze duties arrow gigantic uttered inflamed girth myriad jittery hexagon nail lush reef sushi pastry southern inkling acquire
```

```bash
user@hostname:~$ hdcc -a :9920 wallet init -p
Wallet password:
Seed is:
 potato haunted fuming lordship library vane fever powder zippers fabrics dexterity hoisting emails pebbles each vampire rockets irony summon sailor lemon vipers foxes oneself glide cylinder vehicle mews acoustic

Wallet encrypted with given password
```

* `hdcc wallet unlock` prompts the user for the encryption password
to the wallet, supplied by the `init` command. The wallet must be
initialized and unlocked before any actions can take place.

* `hdcc wallet balance` prints information about your wallet.

Example:
```bash
user@hostname:~$ hdcc wallet balance
Wallet status:
Encrypted, Unlocked
Confirmed Balance:   61516458.00 SC
Unconfirmed Balance: 64516461.00 SC
Exact:               61516457999999999999999999999999 H
```

* `hdcc wallet address` returns a never seen before address for sending
hdccoins to.

* `hdcc wallet send [amount] [dest]` Sends `amount` hdccoins to
`dest`. `amount` is in the form XXXXUU where an X is a number and U is
a unit, for example MS, S, mS, ps, etc. If no unit is given hastings
is assumed. `dest` must be a valid hdccoin address.

* `hdcc wallet lock` locks a wallet. After calling, the wallet must be unlocked
using the encryption password in order to use it further

* `hdcc wallet seeds` returns the list of secret seeds in use by the
wallet. These can be used to regenerate the wallet

* `hdcc wallet addseed` prompts the user for his encryption password,
as well as a new secret seed. The wallet will then incorporate this
seed into itself. This can be used for wallet recovery and merging.

#### Host tasks
* `host config [setting] [value]`

is used to configure hosting.

In version `1.2.2`, sia hosting is configured as follows:

| Setting                  | Value                                           |
| -------------------------|-------------------------------------------------|
| acceptingcontracts       | Yes or No                                       |
| maxduration              | in weeks, at least 12                           |
| collateral               | in SC / TB / Month, 10-1000                     |
| collateralbudget         | in SC                                           |
| maxcollateral            | in SC, max per contract                         |
| mincontractprice         | minimum price in SC per contract                |
| mindownloadbandwidthprice| in SC / TB                                      |
| minstorageprice          | in SC / TB                                      |
| minuploadbandwidthprice  | in SC / TB                                      |

You can call this many times to configure you host before
announcing. Alternatively, you can manually adjust these parameters
inside the `host/config.json` file.

* `hdcc host announce` makes an host announcement. You may optionally
supply a specific address to be announced; this allows you to announce a domain
name. Announcing a second time after changing settings is not necessary, as the
announcement only contains enough information to reach your host.

* `hdcc host -v` outputs some of your hosting settings.

Example:
```bash
user@hostname:~$ hdcc host -v
Host settings:
Storage:      2.0000 TB (1.524 GB used)
Price:        0.000 SC per GB per month
Collateral:   0
Max Filesize: 10000000000
Max Duration: 8640
Contracts:    32
```

* `hdcc hostdb -v` prints a list of all the know active hosts on the
network.

#### Renter tasks
* `hdcc renter upload [filename] [nickname]` uploads a file to the sia
network. `filename` is the path to the file you want to upload, and
nickname is what you will use to refer to that file in the
network. For example, it is common to have the nickname be the same as
the filename.

* `hdcc renter list` displays a list of the your uploaded files
currently on the sia network by nickname, and their filesizes.

* `hdcc renter download [nickname] [destination]` downloads a file
from the sia network onto your computer. `nickname` is the name used
to refer to your file in the sia network, and `destination` is the
path to where the file will be. If a file already exists there, it
will be overwritten.

* `hdcc renter rename [nickname] [newname]` changes the nickname of a
  file.

* `hdcc renter delete [nickname]` removes a file from your list of
stored files. This does not remove it from the network, but only from
your saved list.

* `hdcc renter queue` shows the download queue. This is only relevant
if you have multiple downloads happening simultaneously.

#### Gateway tasks
* `hdcc gateway` prints info about the gateway, including its address and how
many peers it's connected to.

* `hdcc gateway list` prints a list of all currently connected peers.

* `hdcc gateway connect [address:port]` manually connects to a peer and adds it
to the gateway's node list.

* `hdcc gateway disconnect [address:port]` manually disconnects from a peer, but
leaves it in the gateway's node list.

#### Miner tasks
* `hdcc miner status` returns information about the miner. It is only
valid for when hdcd is running.

* `hdcc miner start` starts running the CPU miner on one thread. This
is virtually useless outside of debugging.

* `hdcc miner stop` halts the CPU miner.

#### General commands
* `hdcc consensus` prints the current block ID, current block height, and
current target.

* `hdcc stop` sends the stop signal to hdcd to safely terminate. This
has the same affect as C^c on the terminal.

* `hdcc version` displays the version string of hdcc.

* `hdcc update` checks the server for updates.
