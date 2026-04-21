import { BootstrapService } from "../../../../src/bootstrap.service";
import { CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams } from "../../../../src/command/tx/123-crosspoint-go-done-group-salvo-acknowledge-message/params";
import { CommandParamsValidator } from "../../../../src/command/tx/123-crosspoint-go-done-group-salvo-acknowledge-message/params.validator";
import { LocaleData } from "../../../../src/common/locale-data/locale-data.model";
import { CommandErrorsKeys } from "../../../../src/command/locale-data-keys";


describe('CrossPoint Go Done Group Salvo Acknowledge Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 0
            };
            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 128
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
