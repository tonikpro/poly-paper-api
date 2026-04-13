import { useEffect, useState } from 'react';
import { getPositions } from '../api/client';

interface Position {
  id: string;
  token_id: string;
  outcome: string;
  size: string;
  avg_price: string;
  realized_pnl: string;
  winner: boolean | null;
  question: string;
  is_open: boolean;
}

function StatusBadge({ isOpen, winner }: { isOpen: boolean; winner: boolean | null }) {
  if (isOpen) return <span className="bg-blue-100 text-blue-700 text-xs px-2 py-0.5 rounded-full font-medium">Open</span>;
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Settled</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Won</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Lost</span>;
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

  const open = positions.filter(p => p.is_open);
  const settled = positions.filter(p => !p.is_open);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>

      {positions.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No positions yet.
        </div>
      ) : (
        <>
          {open.length > 0 && (
            <section className="mb-8">
              <h2 className="text-lg font-semibold mb-3 text-gray-700">Open ({open.length})</h2>
              <PositionsTable positions={open} />
            </section>
          )}
          {settled.length > 0 && (
            <section>
              <h2 className="text-lg font-semibold mb-3 text-gray-700">History ({settled.length})</h2>
              <PositionsTable positions={settled} />
            </section>
          )}
        </>
      )}
    </div>
  );
}

function PositionsTable({ positions }: { positions: Position[] }) {
  return (
    <div className="bg-white rounded-lg shadow overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200">
        <thead className="bg-gray-50">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200">
          {positions.map(p => {
            const pnl = parseFloat(p.realized_pnl);
            return (
              <tr key={p.id} className="hover:bg-gray-50">
                <td className="px-4 py-3 max-w-[260px]">
                  {p.question
                    ? <span className="text-sm" title={p.question}>{p.question.length > 60 ? p.question.slice(0, 60) + '…' : p.question}</span>
                    : <span className="text-xs font-mono text-gray-400" title={p.token_id}>{p.token_id.slice(0, 14)}…</span>
                  }
                </td>
                <td className="px-4 py-3 font-medium">{p.outcome}</td>
                <td className="px-4 py-3">{parseFloat(p.size).toFixed(2)}</td>
                <td className="px-4 py-3">{p.avg_price}</td>
                <td className="px-4 py-3">
                  <StatusBadge isOpen={p.is_open} winner={p.winner} />
                </td>
                <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                  {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
