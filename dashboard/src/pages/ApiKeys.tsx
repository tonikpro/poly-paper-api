import { useEffect, useState } from 'react';
import { getEthAddress, getApiKeys, createApiKey, deleteApiKey } from '../api/client';

interface ApiKeyRecord {
  apiKey: string;
  secret: string;
  passphrase: string;
  created_at: string;
}

interface CreatedKey {
  apiKey: string;
  secret: string;
  passphrase: string;
}

export default function ApiKeys() {
  const [ethAddress, setEthAddress] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [showPrivateKey, setShowPrivateKey] = useState(false);
  const [apiKeysList, setApiKeysList] = useState<ApiKeyRecord[]>([]);
  const [createdKey, setCreatedKey] = useState<CreatedKey | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    getEthAddress()
      .then(r => {
        setEthAddress(r.data.eth_address || '');
        setPrivateKey(r.data.private_key || '');
      })
      .catch(() => {});
    loadKeys();
  }, []);

  function loadKeys() {
    getApiKeys()
      .then(r => setApiKeysList(r.data.apiKeys as ApiKeyRecord[] || []))
      .catch(() => {});
  }

  async function handleCreate() {
    setLoading(true);
    try {
      const r = await createApiKey();
      setCreatedKey(r.data);
      loadKeys();
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }

  async function handleDelete(key: string) {
    try {
      await deleteApiKey(key);
      if (createdKey?.apiKey === key) setCreatedKey(null);
      loadKeys();
    } catch {
      // ignore
    }
  }

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
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">API Keys</h2>
          <button onClick={handleCreate} disabled={loading}
            className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 disabled:opacity-50">
            {loading ? 'Creating...' : 'Generate New Key'}
          </button>
        </div>

        {createdKey && (
          <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4 mb-4">
            <h3 className="font-semibold text-yellow-800 mb-2">New API Key Created</h3>
            <p className="text-sm text-yellow-700 mb-3">Save these credentials now - the secret won't be shown again.</p>
            <div className="space-y-1 font-mono text-sm">
              <div><span className="text-gray-600">API Key:</span> {createdKey.apiKey}</div>
              <div><span className="text-gray-600">Secret:</span> {createdKey.secret}</div>
              <div><span className="text-gray-600">Passphrase:</span> {createdKey.passphrase}</div>
            </div>
          </div>
        )}

        {apiKeysList.length === 0 ? (
          <p className="text-gray-500 text-sm">No API keys yet. Generate one to get started.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left py-2">API Key</th>
                <th className="text-right py-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {apiKeysList.map(k => (
                <tr key={k.apiKey} className="border-b">
                  <td className="py-2 font-mono text-sm">{k.apiKey.slice(0, 16)}...{k.apiKey.slice(-8)}</td>
                  <td className="py-2 text-right">
                    <button onClick={() => handleDelete(k.apiKey)}
                      className="text-red-600 hover:underline text-sm">Revoke</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="bg-white rounded-lg shadow p-6">
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
    </div>
  );
}
