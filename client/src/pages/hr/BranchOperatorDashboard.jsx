import { Link } from 'react-router-dom';
import { getUser, operatorBranch } from '../../roles';
import { BRANCH_OPERATOR_TILES } from './branchOperator';

// Landing page for the branch operator. Like HRDashboard it replaces the
// operational dashboard with a map of what this role can do — which is short:
// check attendance and file requests for one branch's employees. Every list
// behind these tiles is already confined to that branch by the server.
export default function BranchOperatorDashboard() {
  const user = getUser();
  const branch = operatorBranch();

  return (
    <>
      <div className="page-header">
        <div>
          <h1>Dasbor Cabang{branch?.name ? ` — ${branch.name}` : ''}</h1>
          <p style={{ color: 'var(--ink-3)', margin: '0.2rem 0 0' }}>
            Selamat datang{user?.username ? `, ${user.username}` : ''}. Pengajuan yang Anda buat
            menunggu persetujuan manajer.
          </p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(230px, 1fr))', gap: '0.8rem' }}>
        {BRANCH_OPERATOR_TILES.map((i) => (
          <Link key={i.to} to={i.to} className="hrd-tile">
            <span className="hrd-tile-icon" aria-hidden="true">{i.icon}</span>
            <span>
              <span className="hrd-tile-label">{i.label}</span>
              <span className="hrd-tile-desc">{i.desc}</span>
            </span>
          </Link>
        ))}
      </div>
    </>
  );
}
