--ErrInvalidInput
print("------------start testErrBalance------------")

local m = require("dnaapi")
wallet = client.new("wallet_test.dat", "pwd", false)
addr = wallet:getAddr()
pubkey = wallet:getPubkey()
assetID = m.getAssetID()

m.togglemining(false)
height = m.getCurrentBlockHeight()
txhash = m.getCoinbaseHashByHeight(height)

while(true)
do
	m.discreteMining(1)
	currentHeight = m.getCurrentBlockHeight()
	print("current height:", currentHeight)
	if ((currentHeight - height) > 10)
	then
		break
	end
end


-- "Invalide transaction UTXO output."
ta = transferasset.new()
input = utxotxinput.new(txhash, 1, 0xffffffff)
output = txoutput.new(assetID, 0, addr)
tx = transaction.new(0x80, 0, ta, 0)
tx:appendtxin(input)
tx:appendtxout(output)
tx:sign(wallet)
tx:hash()
res=m.sendRawTx(tx)
if (res ~= "transaction balance unmatched")
then
	print(res)
	return
else
	print("test balance success")
end

-- "GetReference failed"
ta2 = transferasset.new()
temp=string.rep("00", 31)
zeroHash= temp.."00"
input2 = utxotxinput.new(zeroHash, 1, 0xffffffff)
output2 = txoutput.new(assetID, 1, addr)
tx2 = transaction.new(0x80, 0, ta2, 0)
tx2:appendtxin(input2)
tx2:appendtxout(output2)
tx2:sign(wallet)
tx2:hash()
res=m.sendRawTx(tx2)
if (res ~= "transaction balance unmatched")
then
	print(res)
	return
else
	print("test balance success")
end


-- "input <= output "
ta3 = transferasset.new()
input3 = utxotxinput.new(txhash, 1, 0xffffffff)
output3 = txoutput.new(assetID, 3000*10000*100000000, addr)
tx3 = transaction.new(0x80, 0, ta3, 0)
tx3:appendtxin(input3)
tx3:appendtxout(output3)
tx3:sign(wallet)
tx3:hash()
res=m.sendRawTx(tx3)
if (res ~= "transaction balance unmatched")
then
	print(res)
	return
else
	print("test balance success")
end

-- GetReference failed, refIdx out of range.
ta4 = transferasset.new()
input4 = utxotxinput.new(txhash, 2, 0xffffffff)
output4 = txoutput.new(assetID, 1, addr)
tx4 = transaction.new(0x80, 0, ta4, 0)
tx4:appendtxin(input4)
tx4:appendtxout(output4)
tx4:sign(wallet)
tx4:hash()
res=m.sendRawTx(tx4)
if (res ~= "transaction balance unmatched")
then
	print(res)
	return
else
	print("test balance success")
end

