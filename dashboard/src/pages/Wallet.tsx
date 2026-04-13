import { useEffect, useState } from 'react';
import { getWallet, deposit, withdraw, getStats } from '../api/client';

interface Stats {
  total_pnl: number;
  today_pnl: number;
  month_pnl: number;
  total_bets: number;
  won_bets: number;
  lost_bets: number;
}

function pnlClass(v: number) {
  return v >= 0 ? 'text-green-600' : 'text-red-600';
}
function fmtPnl(v: number) {
  return `${v >= 0 ? '+' : ''}$${v.toFixed(2)}`;
}

export default function Wallet() {
  const [balance, setBalance] = useState('--');
  const [amount, setAmount] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [stats, setStats] = useState<Stats | null>(null);

  const loadData = () => {
    getWallet().then(r => setBalance(r.data.balance || '0')).catch(() => {});
    getStats().then(r => setStats(r.data)).catch(() => {});
  };

  useEffect(() => { loadData(); }, []);

  const handleDeposit = async () => {
    setError(''); setSuccess('');
    try {
      await deposit(amount);
      setSuccess(`Deposited $${amount}`);
      setAmount('');
      loadData();
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
      loadData();
    } catch (err: any) {
      setError(err.response?.data?.error || 'Withdraw failed');
    }
  };

  const winRate = stats && stats.total_bets > 0
    ? Math.round((stats.won_bets / stats.total_bets) * 100)
    : null;
  const pending = stats ? stats.total_bets - stats.won_bets - stats.lost_bets : 0;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Wallet</h1>

      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <div className="text-sm text-gray-500">Current Balance</div>
        <div className="text-4xl font-bold mt-2">${balance}</div>
      </div>

      {/* P&L Statistics */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        {[
          { label: 'Total P&L', value: stats?.total_pnl },
          { label: 'This Month', value: stats?.month_pnl },
          { label: 'Today', value: stats?.today_pnl },
        ].map(({ label, value }) => (
          <div key={label} className="bg-white rounded-lg shadow p-4">
            <div className="text-xs text-gray-500">{label}</div>
            <div className={`text-xl font-bold mt-1 ${value === undefined ? '' : pnlClass(value)}`}>
              {value !== undefined ? fmtPnl(value) : '--'}
            </div>
          </div>
        ))}
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs text-gray-500">Win Rate</div>
          <div className="text-xl font-bold mt-1">
            {winRate !== null ? `${winRate}%` : '--'}
          </div>
          {stats && (
            <div className="text-xs text-gray-400 mt-0.5">
              {stats.won_bets}W / {stats.lost_bets}L{pending > 0 ? ` / ${pending} pending` : ''}
            </div>
          )}
        </div>
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
