import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions } from './options';
import { CommandParamsUtility, CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Go Done Group Salvo Acknowledge Message command
 *
 * This message is issued by the controller in response to a CROSSPOINT GO GROUP SALVO message (Command Byte 121).
 * N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand
 * @extends {CommandBase<CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams>}
 */
export class CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand extends CommandBase<
    CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams,
    CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions
> {
    /**
     *Creates an instance of CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand.
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams} params the command parameters
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions} _options the command options
     * @memberof CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand
     */
    constructor(
        params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams,
        options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions
    ) {
        super(CommandIdentifiers.TX.GENERAL.CROSSPOINT_GO_DONE_GROUP_SALVO_ACKNOWLEDGE_MESSAGE, params, options);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)} - ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return descriptionFor('General');
    }

    /**
     * Builds the commands
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpByteCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - General CrossPoint Go Done Group Salvo Acknowledge Message CMD_123_0X7b
     *
     * + This message is issued by the controller in response to a CROSSPOINT GO GROUP SALVO message (Command Byte 121).
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte | 123 - 0x7b                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |CrossPoint    | CrossPoint status                                                                                                                  |
     * |         |status        | 0 = CrossPoints set                                                                                                                |
     * |         |              | 1 = Stored crosspoints cleared                                                                                                     |
     * |         |              | 2 = No crosspoints to set / clear                                                                                                  |
     * | Byte 2  | Salvo num    | Salvo group number                                                                                                                 |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.options.salvoCrossPointStatus)
            .writeUInt8(this.params.salvoId)
            .toBuffer();
    }

    /**
     * Validate the parameters and throw a ValidationError in case of error
     * @private
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams} params
     * @memberof CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand
     */
    private validateParams(params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();
        if (Object.keys(validationErrors).length > 0) {
            const message = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(CommandErrorsKeys.CREATE_COMMAND_FAILURE)
                .description;
            throw new ValidationError(message, validationErrors);
        }
    }
}
