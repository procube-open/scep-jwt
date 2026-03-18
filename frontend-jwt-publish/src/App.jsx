import React from 'react';
import { Controller, useForm } from "react-hook-form";
import './App.css';
import {
  Box,
  TextField,
  Button,
  Typography,
  InputAdornment,
  IconButton,
  ToggleButton,
  ToggleButtonGroup
} from '@mui/material'
import VisibilityIcon from '@mui/icons-material/Visibility';
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import CircularProgress from '@mui/material/CircularProgress';
import { ToastContainer, toast, Bounce } from 'react-toastify';
import { useTranslation } from "react-i18next";
import LanguageIcon from '@mui/icons-material/Language';
import i18n from "i18next";
import 'react-toastify/dist/ReactToastify.css';

function App() {
  const queryParameters = new URLSearchParams(window.location.search)
  const uid = queryParameters.get("uid")
  const secret = queryParameters.get("secret")
  const { t } = useTranslation();
  const [isRevealSecret, setIsRevealSecret] = React.useState(false);
  const [isIssuing, setIsIssuing] = React.useState(false);
  const [issuedToken, setIssuedToken] = React.useState('');
  const [expiresAt, setExpiresAt] = React.useState('');
  const [language, setLanguage] = React.useState('ja');

  React.useEffect(() => {
    i18n.changeLanguage(language)
  }, [language])

  const toggleSecret = () => {
    setIsRevealSecret((prevState) => !prevState);
  }
  const SecretToggleButton = () => (
    <IconButton
      onClick={toggleSecret}
      children={isRevealSecret ? <VisibilityIcon /> : <VisibilityOffIcon />}
    />
  )
  const LanguageToggleButtons = (props) => {
    const handleChange = (
      event,
      lang,
    ) => {
      setLanguage(lang);
    };


    return (
      <Box {...props}>
        <LanguageIcon sx={{ mt: 1, mr: 1 }} fontSize="large" color="action"/>
        <ToggleButtonGroup
          value={language}
          exclusive
          onChange={handleChange}
        >
          <ToggleButton value="ja">
            {t("jwtweb.japanese")}
          </ToggleButton>
          <ToggleButton value="en">
            {t("jwtweb.english")}
          </ToggleButton>
        </ToggleButtonGroup >
      </Box>
    );
  }
  const {
    handleSubmit,
    control,
  } = useForm({
    mode: "onBlur",
    criteriaMode: "all",
    shouldFocusError: false,
  });

  const onSubmit = async (data) => {
    setIsIssuing(true)
    setIssuedToken('')
    setExpiresAt('')
    const res = await fetch('/api/jwt/issue', {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(data)
    });
    if (res.status === 200) {
      const payload = await res.json()
      setIsIssuing(false)
      setIssuedToken(payload.token)
      setExpiresAt(payload.expires_at)
      toast.success(t("jwtweb.success"), {
        position: "bottom-left",
        autoClose: 5000,
        hideProgressBar: true,
        closeOnClick: true,
        pauseOnHover: true,
        draggable: true,
        progress: undefined,
        theme: "light",
        transition: Bounce,
      })
    }
    else {
      setIsIssuing(false)
      toast.error(await res.text(), {
        position: "bottom-left",
        autoClose: 5000,
        hideProgressBar: true,
        closeOnClick: true,
        pauseOnHover: true,
        draggable: true,
        progress: undefined,
        theme: "light",
        transition: Bounce,
      });
    }
  };

  const copyToken = async () => {
    if (!issuedToken) {
      return;
    }
    try {
      await navigator.clipboard.writeText(issuedToken)
      toast.success(t("jwtweb.copied"), {
        position: "bottom-left",
        autoClose: 3000,
        hideProgressBar: true,
        closeOnClick: true,
        pauseOnHover: true,
        draggable: true,
        progress: undefined,
        theme: "light",
        transition: Bounce,
      })
    } catch {
      toast.error(t("jwtweb.copy_failed"), {
        position: "bottom-left",
        autoClose: 5000,
        hideProgressBar: true,
        closeOnClick: true,
        pauseOnHover: true,
        draggable: true,
        progress: undefined,
        theme: "light",
        transition: Bounce,
      })
    }
  }

  return (
    <Box
      component="form"
      sx={{
        width: 1,
        height: '100vh',
        backgroundColor: "#efefef",
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
      }}
      onSubmit={handleSubmit(onSubmit)}
    >
      <ToastContainer />
      <Box sx={{
        p: 3,
        m: 2,
        borderRadius: 2,
        backgroundColor: "#ffffff",
        display: "flex",
        flexDirection: "column",
      }}>
        <Typography variant="h5" sx={{ width: 1,ml:2 }}>
          {t("jwtweb.title")}
        </Typography>
        <Box sx={{ width: 1, diplay: "flex-inline" }}>
          <Controller
            name="uid"
            control={control}
            rules={{
              required: t("jwtweb.required")
            }}
            render={({
              field: { onChange, onBlur, value },
              fieldState: { error },
            }) => (
              <TextField
                label={t("jwtweb.uid")}
                required
                value={value}
                defaultValue={uid}
                sx={{
                  width: "45%",
                }}
                variant="outlined"
                margin="dense"
                onChange={onChange}
                onBlur={onBlur}
                error={Boolean(error)}
                helperText={error?.message}
              />
            )}
          />
          <Controller
            name="secret"
            control={control}
            rules={{
              required: t("jwtweb.required")
            }}
            render={({
              field: { onChange, onBlur, value },
              fieldState: { error },
            }) => (
              <TextField
                label={t("jwtweb.secret")}
                required
                value={value}
                defaultValue={secret}
                sx={{
                  width: "45%",
                  ml: 2
                }}
                variant="outlined"
                margin="dense"
                onChange={onChange}
                onBlur={onBlur}
                error={Boolean(error)}
                helperText={error?.message}
                type={isRevealSecret ? 'text' : 'password'}
                InputProps={{
                  endAdornment: (
                    <InputAdornment position="end">
                      <SecretToggleButton />
                    </InputAdornment>
                  )
                }}
              />
            )}
          />
        </Box>
        <Box sx={{ mt: 2, display: "flex", alignItems: "flex-start", gap: 1 }}>
          <Box sx={{ flex: 1 }}>
            <Typography variant="subtitle2" sx={{ ml: 1 }}>
              {t("jwtweb.token")}
            </Typography>
            <Typography
              variant="body2"
              sx={{
                mt: 1,
                ml: 1,
                p: 1.5,
                backgroundColor: "#f6f6f6",
                borderRadius: 1,
                wordBreak: "break-all",
                minHeight: 96,
              }}
            >
              {issuedToken || t("jwtweb.no_token")}
            </Typography>
            <Typography variant="body2" sx={{ mt: 1, ml: 1, color: "warning.dark" }}>
              {t("jwtweb.token_notice")}
            </Typography>
          </Box>
          <IconButton
            aria-label="copy token"
            onClick={copyToken}
            disabled={!issuedToken}
            sx={{ mt: 3 }}
          >
            <ContentCopyIcon />
          </IconButton>
        </Box>
        {expiresAt && (
          <Typography variant="body2" sx={{ mt: 1, ml: 1, color: "text.secondary" }}>
            {t("jwtweb.expires")}: {new Date(expiresAt).toLocaleString()}
          </Typography>
        )}
        <Button
          startIcon={isIssuing && <CircularProgress size={20} color="inherit" />}
          sx={{ mt: 2 }}
          type="submit"
          disabled={isIssuing}
          color="primary"
          variant="contained"
          size="large"
        >
          {t("jwtweb.issue")}
        </Button>
      </Box>
      <LanguageToggleButtons sx={{ mr: 2, justifyContent: "flex-end", display: "inline-flex" }} />
    </Box>
  );
}

export default App;
