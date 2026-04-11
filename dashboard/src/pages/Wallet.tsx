import { useEffect, useState } from 'react';
import { getWallet, deposit, withdraw } from '../api/client';

export default function Wallet() {
  const [balance, setBalance] = useState('--');
  const [amount, setAmount] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  const loadWallet = () => {
    getWallet().then(r => setBalance(r.data.balance || '0')).catch(() => {});
  };

  useEffect(() => { loadWallet(); }, []);

  const handleDeposit = async () => {
    setError(''); setSuccess('');
    try {
      await deposit(amount);
      setSuccess(`Deposited $${amount}`);
      setAmount('');
      loadWallet();
    } catch (err: any) {
      setError(err.response?.data?.error || 'Deposit failed');
    }
  };

  const handleWithdraw = async () => {
    setError(''); setSuccess('');
    try {
      await withdraw(amount);
      setSuccess(`Withdrew $${amount}`);
      setAmount('');
      loadWallet();
    } catch (err: any) {
      setError(err.response?.data?.error || 'Withdraw failed');
    }
  };

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Wallet</h1>
      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <div className="text-sm text-gray-500">Current Balance</div>
        <div className="text-4xl font-bold mt-2">${balance}</div>
      </div>

      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-semibold mb-4">Manage Funds</h2>
        {error && <div className="bg-red-50 text-red-600 p-3 rounded text-sm mb-4">{error}</div>}
        {success && <div className="bg-green-50 text-green-600 p-3 rounded text-sm mb-4">{success}</div>}
        <div className="flex items-center space-x-4">
          <input type="number" placeholder="Amount" value={amount} onChange={e => setAmount(e.target.value)}
            className="flex-1 px-4 py-2 border rounded focus:outline-none focus:ring-2 focus:ring-blue-500" min="0" step="0.01" />
          <button onClick={handleDeposit} className="bg-green-600 text-white px-6 py-2 rounded hover:bg-green-700">
            Deposit
          </button>
          <button onClick={handleWithdraw} className="bg-red-600 text-white px-6 py-2 rounded hover:bg-red-700">
            Withdraw
          </button>
        </div>
        <p className="mt-4 text-sm text-gray-500">
          Virtual funds for paper trading. No real money involved.
        </p>
      </div>
    </div>
  );
}
