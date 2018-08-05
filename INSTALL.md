# Ubuntu

Install Go 1.9
---
wget https://dl.google.com/go/go1.9.4.linux-amd64.tar.gz  
tar zxvf go1.9.4.linux-amd64.tar.gz  
mv go /usr/local/  
ln -s /usr/local/go/bin/go /usr/bin/go  
ln -s /usr/local/go/bin/gofmt /usr/bin/gofmt  

Install dependencies
---
make dependencies

Setup data directory
---
mkdir ~/hyperspace  
cd ~/hyperspace  

Run standard node
---
hsd

OR run pool node:
---

Configure db info, port number, etc
---
cp sampleconfig/sia.yml ~/hyperspace  
vim ~/hyperspace/sia.yml

Install mysql if necessary
---
apt install mysql-server

Run pool
---
hsd -M cgtwp

Note: make sure you increase the number of open files you can have at a time, or you will have problems with sockets and log files
