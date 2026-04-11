import { useEffect, useState } from 'react';
import { getOrders } from '../api/client';

interface Order {
  id: string;
  token_id: string;
  side: string;
  price: string;
  original_size: string;
  size_matched: string;
  status: string;
  order_type: string;
  created_at: string;
}

export default function Orders() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getOrders({ limit: 50 })
      .then(r => setOrders(r.data.orders || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Orders</h1>
      {orders.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No orders yet. Place orders using your bot.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Filled</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Token</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {orders.map(o => (
                <tr key={o.id}>
                  <td className={`px-4 py-3 font-medium ${o.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>{o.side}</td>
                  <td className="px-4 py-3">{o.price}</td>
                  <td className="px-4 py-3">{o.original_size}</td>
                  <td className="px-4 py-3">{o.size_matched}</td>
                  <td className="px-4 py-3"><StatusBadge status={o.status} /></td>
                  <td className="px-4 py-3 text-xs">{o.order_type}</td>
                  <td className="px-4 py-3 text-xs font-mono truncate max-w-[120px]" title={o.token_id}>{o.token_id?.slice(0, 12)}...</td>
                  <td className="px-4 py-3 text-xs text-gray-500">{new Date(o.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    LIVE: 'bg-blue-100 text-blue-800',
    MATCHED: 'bg-green-100 text-green-800',
    CANCELED: 'bg-gray-100 text-gray-800',
  };
  return (
    <span className={`px-2 py-1 rounded text-xs font-medium ${colors[status] || 'bg-gray-100'}`}>
      {status}
    </span>
  );
}
