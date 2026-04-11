import { useEffect, useState } from 'react';
import { getPositions } from '../api/client';

interface Position {
  id: string;
  token_id: string;
  outcome: string;
  size: string;
  avg_price: string;
  realized_pnl: string;
}

export default function Positions() {
  const [positions, setPositions] = useState<Position[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getPositions()
      .then(r => setPositions(r.data.positions || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>
      {positions.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No open positions.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Token</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized PnL</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {positions.map(p => (
                <tr key={p.id}>
                  <td className="px-4 py-3 text-xs font-mono truncate max-w-[160px]" title={p.token_id}>{p.token_id?.slice(0, 16)}...</td>
                  <td className="px-4 py-3">{p.outcome}</td>
                  <td className="px-4 py-3">{p.size}</td>
                  <td className="px-4 py-3">{p.avg_price}</td>
                  <td className={`px-4 py-3 ${parseFloat(p.realized_pnl) >= 0 ? 'text-green-600' : 'text-red-600'}`}>
                    ${p.realized_pnl}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
