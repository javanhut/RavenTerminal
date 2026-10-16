#include "_cgo_export.h"

void glfwSetPreeditCallbackCB(GLFWwindow *window) {
  glfwSetPreeditCallback(window, (GLFWpreeditfun)goPreeditCB);
}
