Ubuntu
------

# Install Go 1.9
wget https://dl.google.com/go/go1.9.4.linux-amd64.tar.gz
tar zxvf go1.9.4.linux-amd64.tar.gz
mv go /usr/local/
ln -s /usr/local/go/bin/go /usr/bin/go
ln -s /usr/local/go/bin/gofmt /usr/bin/gofmt

# Install dependencies
make dependencies

# Setup data directory
mkdir ~/hdc
cp hdc.yml ~/hdc
cd ~/hdc

# Run standard node
hdcd

# OR run pool node:

# Configure db info, port number, etc
vim ~/hdc/hdc.yml

# Install mysql if necessary
apt install mysql-server

# Run pool
hdcd -M cghtwp
