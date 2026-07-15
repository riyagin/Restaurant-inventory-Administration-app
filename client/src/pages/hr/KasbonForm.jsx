import { useNavigate } from 'react-router-dom';
import KasbonFormModal from './KasbonFormModal';

// Standalone route (/hr/kasbon/new) kept for back-compat / deep links.
// The form itself now lives in KasbonFormModal (shown as an overlay); this page
// just renders it and returns to the dashboard on close/save.
export default function KasbonForm() {
  const navigate = useNavigate();
  const back = () => navigate('/hr/kasbon');
  return <KasbonFormModal onClose={back} onSaved={back} />;
}
