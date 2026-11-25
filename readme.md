# wallet API

| method | path | does | status |
| --------------- | --------------- | --------------- | --------------- |
| post | /v1/wallets | open a wallet for an owner, in one currency | 201 |
| get | /v1/wallets?owner_id= | lists owner's wallets | 200 |
| get | /v1/wallets/{id} | read a wallet | 200 |
| get | /v1/wallets/{id}/balance | its balance, derived from the ledger | 200 |
| post | /v1/wallets/{id}/deposits | add money from outside | 201 |
| get | /v1/wallets/{id}/entries | its ledger entries, a page at a time | 200 |

