<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Category, Point, PointDraft } from '@/api/tracking'
import * as api from '@/api/tracking'
import BasicDefinitionStep from './BasicDefinitionStep.vue'
import TriggerStatisticsStep from './TriggerStatisticsStep.vue'
import PropertySchemaStep from './PropertySchemaStep.vue'
import CollaborationPublishStep from './CollaborationPublishStep.vue'

const props=defineProps<{projectId:number;categories:Category[];initialPoint?:Point;canMutate:boolean}>()
const emit=defineEmits<{saved:[Point];published:[number];cancel:[]}>()
const step=ref(0), error=ref(''), busy=ref(false), savedId=ref<number|undefined>(props.initialPoint?.id)
const blank=():PointDraft=>({category_id:0,code:'',name:'',description:'',behavior_type:'',development_state:'PENDING',trigger_page:'',trigger_timing:'',trigger_count_rule:'',abnormal_trigger_rule:'',count_pv:true,count_uv:true,uv_mode:'APPROXIMATE',time_basis:'CLIENT',supported_platforms:['web'],supported_channels:[],product_owner:'',developer_owner:'',requirement_url:'',properties:[]})
function fromPoint(p?:Point):PointDraft { if(!p)return blank(); const {id:_,project_id:__,category:___,category_path:____,definition_state:_____,created_by:______,updated_by:_______,published_version:________,deprecation_reason:_________,replacement_point_id:__________,...draft}=p; return JSON.parse(JSON.stringify(draft)) as PointDraft }
const model=ref<PointDraft>(fromPoint(props.initialPoint))
const fingerprint=(value:PointDraft)=>JSON.stringify(value)
const savedFingerprint=ref(savedId.value?fingerprint(model.value):'')
const dirty=computed(()=>!savedId.value||savedFingerprint.value!==fingerprint(model.value))
watch(()=>props.projectId,()=>{model.value=blank();savedId.value=undefined;savedFingerprint.value='';step.value=0;error.value=''})
const scene=computed(()=>props.categories.find(c=>c.id===model.value.category_id&&c.level==='SCENE'&&c.active))
function validate(publish=false){if(!scene.value)return '点位必须归属到业务场景（Scene）';if(!/^[a-z][a-z0-9_]{2,127}$/.test(model.value.code)||!model.value.name)return '请填写合法的点位编码和名称';const codes=new Set<string>();for(const p of model.value.properties){if(!p.code||codes.has(p.code))return '属性编码不能为空且不能重复';codes.add(p.code);if(publish&&(!p.name||!p.type))return '发布前请补全属性名称和类型';if(p.type==='ENUM'&&!p.enum_values?.length)return '枚举属性至少需要一个枚举值';if(p.type==='ARRAY'&&!p.array_item_type)return '数组属性必须选择元素类型'}if(publish&&(!model.value.behavior_type||!model.value.supported_platforms.length))return '发布前请选择行为类型和支持平台';return ''}
async function save(){error.value=validate();if(error.value||!props.canMutate)return;busy.value=true;try{const p=savedId.value?await api.updateDraft(props.projectId,savedId.value,model.value):await api.createPoint(props.projectId,model.value);savedId.value=p.id;savedFingerprint.value=fingerprint(model.value);emit('saved',p)}catch(e){error.value=e instanceof Error?e.message:'保存失败'}finally{busy.value=false}}
async function publish(){error.value=validate(true);if(error.value||!props.canMutate||!savedId.value)return;if(dirty.value){error.value='请先保存当前修改，再发布点位';return}busy.value=true;try{await api.publishPoint(props.projectId,savedId.value);emit('published',savedId.value)}catch(e){error.value=e instanceof Error?e.message:'发布失败'}finally{busy.value=false}}
</script>
<template><div class="point-wizard"><ol class="wizard-steps"><li v-for="(name,i) in ['基础定义','触发与统计','属性 Schema','协作与发布']" :key="name" :class="{active:i===step,done:i<step}"><button type="button" @click="step=i"><span>{{ i+1 }}</span>{{ name }}</button></li></ol><div v-if="error" class="form-alert" role="alert">{{ error }}</div><BasicDefinitionStep v-if="step===0" v-model="model" :categories="categories"/><TriggerStatisticsStep v-else-if="step===1" v-model="model"/><PropertySchemaStep v-else-if="step===2" v-model="model"/><CollaborationPublishStep v-else v-model="model" :categories="categories" :project-id="projectId"/><footer class="wizard-actions"><button type="button" class="secondary-button" @click="emit('cancel')">取消</button><button v-if="step>0" type="button" class="secondary-button" @click="step--">上一步</button><button v-if="step<3" type="button" class="secondary-button" @click="step++">下一步</button><button v-if="canMutate" data-test="save-draft" type="button" class="secondary-button" :disabled="busy" @click="save">保存草稿</button><button v-if="canMutate" data-test="publish" type="button" class="primary-button" :disabled="busy||!savedId" @click="publish">发布点位</button></footer></div></template>
