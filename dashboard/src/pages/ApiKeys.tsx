import { useEffect, useState } from 'react';
import { getEthAddress } from '../api/client';

interface CreatedKey {
  apiKey: string;
  secret: string;
  passphrase: string;
}

export default function ApiKeys() {
  const [ethAddress, setEthAddress] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [createdKey, setCreatedKey] = useState<CreatedKey | null>(null);
  const [showPrivateKey, setShowPrivateKey] = useState(false);

  useEffect(() => {
    getEthAddress()
      .then(r => {
        setEthAddress(r.data.eth_address || '');
        setPrivateKey(r.data.private_key || '');
      })
      .catch(() => {});
  }, []);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">API Keys & Bot Config</h1>

      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Ethereum Wallet</h2>
        <div className="space-y-3">
          <div>
            <label className="text-sm text-gray-500">ETH Address</label>
            <div className="font-mono text-sm bg-gray-50 p-2 rounded break-all">{ethAddress || '...'}</div>
          </div>
          <div>
            <label className="text-sm text-gray-500">Private Key (for bot config)</label>
            <div className="flex items-center space-x-2">
              <div className="flex-1 font-mono text-sm bg-gray-50 p-2 rounded break-all">
                {showPrivateKey ? privateKey : '********'}
              </div>
              <button onClick={() => setShowPrivateKey(!showPrivateKey)}
                className="text-sm text-blue-600 hover:underline whitespace-nowrap">
                {showPrivateKey ? 'Hide' : 'Show'}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Bot Setup Guide</h2>
        <div className="bg-gray-900 text-green-400 rounded p-4 text-sm font-mono overflow-x-auto">
          <pre>{`from py_clob_client.client import ClobClient

# Paper trading configuration
host = "http://localhost:8080/clob"
key = "${privateKey || '<your_private_key>'}"
chain_id = 137

client = ClobClient(host, key=key, chain_id=chain_id)

# Create API credentials (first time only)
creds = client.create_or_derive_api_creds()
client.set_api_creds(creds)

# Now use as normal - orders go to paper trading
print(client.get_balance_allowance())`}</pre>
        </div>
      </div>

      {createdKey && (
        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-6 mb-6">
          <h3 className="text-lg font-semibold text-yellow-800 mb-2">API Key Created</h3>
          <p className="text-sm text-yellow-700 mb-4">Save these credentials - the secret won't be shown again.</p>
          <div className="space-y-2 font-mono text-sm">
            <div><span className="text-gray-600">API Key:</span> {createdKey.apiKey}</div>
            <div><span className="text-gray-600">Secret:</span> {createdKey.secret}</div>
            <div><span className="text-gray-600">Passphrase:</span> {createdKey.passphrase}</div>
          </div>
        </div>
      )}
    </div>
  );
}
