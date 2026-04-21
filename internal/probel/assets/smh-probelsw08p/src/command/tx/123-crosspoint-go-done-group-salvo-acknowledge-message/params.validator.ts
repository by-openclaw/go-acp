import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams } from './params';

/**
 * CrossPoint Go Done Group Salvo Acknowledge Message command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isSalvoIdOutOfRange()) {
            errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the salvoId is out of range
     * + false = salvoId is included within the limits
     * + true = salvoId [0-127] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSalvoIdOutOfRange(): boolean {
        return this.data.salvoId < 0 || this.data.salvoId > 127;
    }
}
